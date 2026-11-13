// SPDX-License-Identifier: AGPL-3.0-only
import "fake-indexeddb/auto";
import test from "node:test";
import assert from "node:assert/strict";
import * as pgp from "../../internal/webui/assets/vendor/openpgp/openpgp.min.mjs";
import { VaultStore } from "../../internal/webui/assets/crypto/store.mjs";
import {
  LocalVault,
  cryptoConfig,
} from "../../internal/webui/assets/crypto/vault.mjs";
import {
  textEntity,
  composeEntity,
  readAttachments,
  encryptedEntity,
  signedEntity,
  extractEncrypted,
  parseLocalMime,
} from "../../internal/webui/assets/crypto/mime.mjs";
const password = "Synthetic mail key passphrase";
globalThis.fetch = () => {
  throw new Error("Client crypto attempted network access");
};
let sequence = 0;
async function person(address) {
  const vault = new LocalVault(
    new VaultStore(`SYNTHETIC_MAIL_CRYPTO_${++sequence}`),
  );
  const key = await vault.generate("Synthetic", address, password);
  await vault.trust(address, key.fingerprint, key.fingerprint);
  await vault.unlock(key.fingerprint, password);
  return { vault, key, address };
}
async function trust(person, contact) {
  await person.vault.importPublic(contact.key.publicKey);
  await person.vault.trust(
    contact.address,
    contact.key.fingerprint,
    contact.key.fingerprint,
  );
}
test("signed encrypted MIME round-trip, own Sent copy, Bcc key privacy and explicit sender trust", async () => {
  const alice = await person("alice@example.test"),
    bob = await person("bob@example.test"),
    bcc = await person("hidden@example.test");
  try {
    await trust(alice, bob);
    await trust(alice, bcc);
    const plain = textEntity("Zażółć gęślą\n<script>untrusted mail</script>");
    const encrypted = await alice.vault.encryptSigned(plain, alice.address, [
      bob.address,
      bcc.address,
    ]);
    const message = await pgp.readMessage({ armoredMessage: encrypted });
    assert.ok(
      message
        .getEncryptionKeyIDs()
        .every((id) => id.toHex() === "0000000000000000"),
    );
    const outer = new TextEncoder().encode(encryptedEntity(encrypted));
    assert.equal(new TextDecoder().decode(outer).includes("Zażółć"), false);
    const extracted = await extractEncrypted(outer);
    const unknown = await bob.vault.decryptSigned(extracted, alice.address);
    assert.equal(unknown.provenance, "client-decrypted");
    assert.deepEqual(unknown.data, plain);
    assert.deepEqual(
      unknown.signatures.map((s) => s.status),
      ["unknown-key"],
    );
    await bob.vault.importPublic(alice.key.publicKey);
    const discovered = await bob.vault.decryptSigned(extracted, alice.address);
    assert.equal(discovered.signatures[0].status, "valid");
    assert.equal(discovered.signatures[0].trustedSender, false);
    await trust(bob, alice);
    const verified = await bob.vault.decryptSigned(extracted, alice.address);
    assert.equal(verified.signatures[0].trustedSender, true);
    assert.equal(
      (await bob.vault.decryptSigned(extracted, "forged@example.test"))
        .signatures[0].trustedSender,
      false,
    );
    assert.deepEqual(
      (await alice.vault.decryptSigned(extracted, alice.address)).data,
      plain,
    );
    assert.deepEqual(
      (await bcc.vault.decryptSigned(extracted, alice.address)).data,
      plain,
    );
    const parsed = await parseLocalMime(verified.data);
    assert.equal(parsed.text, "Zażółć gęślą\n<script>untrusted mail</script>");
  } finally {
    await Promise.all([
      alice.vault.close(),
      bob.vault.close(),
      bcc.vault.close(),
    ]);
  }
});
test("one missing recipient, wrong sender and lock prevent encrypted output", async () => {
  const alice = await person("alice2@example.test"),
    bob = await person("bob2@example.test");
  try {
    const plain = textEntity("No silent downgrade");
    await assert.rejects(
      alice.vault.encryptSigned(plain, alice.address, [bob.address]),
      { code: "untrusted-recipient" },
    );
    await trust(alice, bob);
    await assert.rejects(
      alice.vault.encryptSigned(plain, alice.address, [
        bob.address,
        "unknown@example.test",
      ]),
      { code: "untrusted-recipient" },
    );
    await assert.rejects(
      alice.vault.encryptSigned(plain, bob.address, [alice.address]),
      { code: "sender-key-mismatch" },
    );
    const pending = alice.vault.encryptSigned(plain, alice.address, [
      bob.address,
    ]);
    alice.vault.lock();
    await assert.rejects(pending, { code: "locked" });
    await assert.rejects(
      alice.vault.encryptSigned(plain, alice.address, [bob.address]),
      { code: "locked" },
    );
  } finally {
    await alice.vault.close();
    await bob.vault.close();
  }
});
test("corrupted signatures differ from unknown signers; unsigned mail stays unsigned", async () => {
  const alice = await person("alice3@example.test"),
    bob = await person("bob3@example.test");
  try {
    await trust(bob, alice);
    const privateKey = await pgp.decryptKey({
      privateKey: await pgp.readPrivateKey({
        armoredKey: await alice.vault.exportPrivate(alice.key.fingerprint),
      }),
      passphrase: password,
      config: cryptoConfig,
    });
    const publicKey = await pgp.readKey({ armoredKey: bob.key.publicKey });
    const signed = await pgp.sign({
      message: await pgp.createMessage({ binary: textEntity("Original") }),
      signingKeys: privateKey,
      format: "object",
      config: cryptoConfig,
    });
    signed.getLiteralData()[0] ^= 1;
    const bad = await pgp.encrypt({
      message: signed,
      encryptionKeys: publicKey,
      format: "armored",
      config: cryptoConfig,
    });
    const result = await bob.vault.decryptSigned(bad, alice.address);
    assert.equal(result.signatures[0].status, "invalid");
    assert.equal(result.signatures[0].trustedSender, false);
    const unsigned = await pgp.encrypt({
      message: await pgp.createMessage({ binary: textEntity("Unsigned") }),
      encryptionKeys: publicKey,
      format: "armored",
      config: cryptoConfig,
    });
    assert.deepEqual(
      (await bob.vault.decryptSigned(unsigned, alice.address)).signatures,
      [],
    );
    await assert.rejects(
      bob.vault.decryptSigned(
        bad.replace("BEGIN PGP MESSAGE", "BEGIN CORRUPTED"),
        alice.address,
      ),
      { code: "decryption-failed" },
    );
  } finally {
    await alice.vault.close();
    await bob.vault.close();
  }
});
test("MIME parser rejects excess size/depth/parts and ambiguous envelope types", async () => {
  await assert.rejects(parseLocalMime(new Uint8Array((4 << 20) + 1)), {
    code: "message-limit",
  });
  await assert.rejects(
    extractEncrypted(
      new TextEncoder().encode("Content-Type: text/plain\r\n\r\nNot encrypted"),
    ),
    { code: "unsupported-encryption" },
  );
  let nested = "Content-Type: text/plain\r\n\r\nEnd";
  for (let n = 0; n < 15; n++)
    nested = `Content-Type: multipart/mixed; boundary=b${n}\r\n\r\n--b${n}\r\n${nested}\r\n--b${n}--\r\n`;
  await assert.rejects(parseLocalMime(new TextEncoder().encode(nested)), {
    code: "invalid-mime",
  });
  const wide = `Content-Type: multipart/mixed; boundary=wide\r\n\r\n${"--wide\r\nContent-Type: text/plain\r\n\r\nPart\r\n".repeat(101)}--wide--\r\n`;
  await assert.rejects(parseLocalMime(new TextEncoder().encode(wide)), {
    code: "invalid-mime",
  });
  assert.throws(() => textEntity("\0"), { code: "message-limit" });
});

test("encrypted multipart attachments preserve Unicode names and exact file octets", async () => {
  const alice = await person("attachments@example.test");
  try {
    const bytes = new Uint8Array([
      0, 255, 13, 10, 60, 115, 99, 114, 105, 112, 116, 62,
    ]);
    const mime = composeEntity("Body", [
      { name: "Zażółć 'notes'.bin", data: bytes },
    ]);
    const encrypted = await alice.vault.encryptSigned(mime, alice.address, [
      alice.address,
    ]);
    assert.equal(encrypted.includes("Zażółć"), false);
    const decrypted = await alice.vault.decryptSigned(encrypted, alice.address);
    const parsed = await parseLocalMime(decrypted.data);
    assert.equal(parsed.attachments.length, 1);
    assert.equal(parsed.attachments[0].filename, "Zażółć 'notes'.bin");
    assert.deepEqual(new Uint8Array(parsed.attachments[0].content), bytes);
    assert.equal(decrypted.signatures[0].trustedSender, true);
    assert.throws(
      () => composeEntity("Body", [{ name: "bad\r\nHeader.bin", data: bytes }]),
      { code: "invalid-attachment-name" },
    );
    assert.throws(
      () => composeEntity("Body", [{ name: "../path", data: bytes }]),
      { code: "invalid-attachment-name" },
    );
    assert.throws(
      () =>
        composeEntity("Body", [
          { name: "large", data: new Uint8Array((1 << 20) + 1) },
        ]),
      { code: "attachment-limit" },
    );
    assert.throws(
      () =>
        composeEntity(
          "Body",
          Array.from({ length: 11 }, () => ({ name: "tiny", data: bytes })),
        ),
      { code: "attachment-limit" },
    );
    let reads = 0;
    await assert.rejects(
      readAttachments([
        {
          name: "large",
          size: (1 << 20) + 1,
          arrayBuffer() {
            reads++;
            throw new Error("must not read oversized file");
          },
        },
      ]),
      { code: "attachment-limit" },
    );
    assert.equal(reads, 0);
  } finally {
    await alice.vault.close();
  }
});

test("binary OpenPGP/MIME preserves ciphertext octets through base64 and quoted-printable transport", async () => {
  const alice = await person("binary@example.test");
  try {
    const secret = await pgp.decryptKey({
      privateKey: await pgp.readPrivateKey({
        armoredKey: await alice.vault.exportPrivate(alice.key.fingerprint),
      }),
      passphrase: password,
      config: cryptoConfig,
    });
    const plain = composeEntity("Binary legacy transport: Zażółć", [
      { name: "octets.bin", data: new Uint8Array([0, 255, 128, 13, 10]) },
    ]);
    const binary = await pgp.encrypt({
      message: await pgp.createMessage({ binary: plain }),
      encryptionKeys: secret.toPublic(),
      signingKeys: secret,
      format: "binary",
      config: cryptoConfig,
    });
    for (const transfer of ["base64", "quoted-printable"]) {
      const body =
        transfer === "base64"
          ? Buffer.from(binary)
              .toString("base64")
              .match(/.{1,76}/g)
              .join("\r\n")
          : Array.from(
              binary,
              (n) => "=" + n.toString(16).padStart(2, "0").toUpperCase(),
            ).join("=\r\n");
      const raw = new TextEncoder().encode(
        `Content-Type: multipart/encrypted; protocol="application/pgp-encrypted"; boundary="binary"\r\n\r\n--binary\r\nContent-Type: application/pgp-encrypted\r\n\r\nVersion: 1\r\n--binary\r\nContent-Type: application/octet-stream\r\nContent-Transfer-Encoding: ${transfer}\r\n\r\n${body}\r\n--binary--\r\n`,
      );
      const extracted = await extractEncrypted(raw);
      assert.ok(extracted instanceof Uint8Array);
      assert.deepEqual(extracted, binary);
      const result = await alice.vault.decryptSigned(extracted, alice.address);
      assert.deepEqual(result.data, plain);
      assert.equal(result.signatures[0].trustedSender, true);
      const parsed = await parseLocalMime(result.data);
      assert.deepEqual(
        new Uint8Array(parsed.attachments[0].content),
        new Uint8Array([0, 255, 128, 13, 10]),
      );
    }
    const corrupted = new Uint8Array(binary);
    corrupted[corrupted.length - 1] ^= 1;
    await assert.rejects(alice.vault.decryptSigned(corrupted, alice.address), {
      code: "decryption-failed",
    });
    alice.vault.lock();
    await assert.rejects(alice.vault.decryptSigned(binary, alice.address), {
      code: "locked",
    });
  } finally {
    await alice.vault.close();
  }
});

test("local detached signing preserves exact MIME, sender binding, locks and attachments without network", async () => {
  const alice = await person("signer@example.test");
  try {
    const input = composeEntity("Visible signed text: Zażółć\nFrom example", [
      { name: "receipt.bin", data: new Uint8Array([0, 255, 13, 10, 42]) },
    ]);
    const original = input.slice();
    const pending = alice.vault.signDetached(input, alice.address);
    input.fill(0); // A caller mutation while signing must not alter signed bytes.
    const signed = await pending;
    assert.deepEqual(signed.data, original);
    const { extractSigned } = await import(
      "../../internal/webui/assets/crypto/signed-mime.mjs"
    );
    const raw = new TextEncoder().encode(
      signedEntity(signed.data, signed.signature),
    );
    const extracted = await extractSigned(raw);
    assert.deepEqual(extracted.data, original);
    const result = await alice.vault.verifyDetached(
      extracted.data,
      extracted.signature,
      alice.address,
    );
    assert.equal(result.signatures[0].trustedSender, true);
    const parsed = await parseLocalMime(extracted.data);
    assert.match(parsed.text, /Visible signed text/);
    assert.deepEqual(
      new Uint8Array(parsed.attachments[0].content),
      new Uint8Array([0, 255, 13, 10, 42]),
    );
    await assert.rejects(
      alice.vault.signDetached(original, "forged@example.test"),
      { code: "sender-key-mismatch" },
    );
    assert.throws(
      () =>
        signedEntity(
          new TextEncoder().encode(
            "Content-Type: text/plain\n\nwrong endings\n",
          ),
          signed.signature,
        ),
      { code: "invalid-mime" },
    );
    assert.throws(
      () =>
        signedEntity(
          new TextEncoder().encode("Content-Type: text/plain\r\n\r\nraw é\r\n"),
          signed.signature,
        ),
      { code: "invalid-mime" },
    );
    const whileLocking = alice.vault.signDetached(original, alice.address);
    alice.vault.lock();
    await assert.rejects(whileLocking, { code: "locked" });
    await assert.rejects(alice.vault.signDetached(original, alice.address), {
      code: "locked",
    });
  } finally {
    await alice.vault.close();
  }
});
