// SPDX-License-Identifier: AGPL-3.0-only
import "fake-indexeddb/auto";
import test from "node:test";
import assert from "node:assert/strict";
import * as pgp from "../../internal/webui/assets/vendor/openpgp/openpgp.min.mjs";
import {
  LocalVault,
  cryptoConfig,
} from "../../internal/webui/assets/crypto/vault.mjs";
import { VaultStore } from "../../internal/webui/assets/crypto/store.mjs";
import { extractSigned } from "../../internal/webui/assets/crypto/signed-mime.mjs";
import {
  composeEntity,
  parseLocalMime,
} from "../../internal/webui/assets/crypto/mime.mjs";
const encode = (value) => new TextEncoder().encode(value);
function envelope(data, signature) {
  return encode(
    `From: signer@example.test\r\nContent-Type: multipart/signed; protocol="application/pgp-signature"; micalg=pgp-sha512; boundary="signed-boundary"\r\n\r\nUnsigned preamble must never render\r\n--signed-boundary\r\n${new TextDecoder().decode(data)}\r\n--signed-boundary\r\nContent-Type: application/pgp-signature\r\n\r\n${signature.trimEnd()}\r\n--signed-boundary--\r\nUnsigned epilogue must never render\r\n`,
  );
}
test("detached MIME verifies original octets, separates identity and rejects tampered bytes", async () => {
  globalThis.fetch = () => {
    throw new Error("verification attempted network access");
  };
  const password = "Synthetic detached signing passphrase";
  const signer = new LocalVault(new VaultStore("SYNTHETIC_DETACHED_SIGNER"));
  const reader = new LocalVault(new VaultStore("SYNTHETIC_DETACHED_READER"));
  try {
    const key = await signer.generate(
      "Detached signer",
      "signer@example.test",
      password,
    );
    const privateKey = await pgp.decryptKey({
      privateKey: await pgp.readPrivateKey({
        armoredKey: await signer.exportPrivate(key.fingerprint),
      }),
      passphrase: password,
      config: cryptoConfig,
    });
    const data = composeEntity("Unicode Zażółć\nActual body", [
      { name: "octets.bin", data: new Uint8Array([0, 255, 13, 10]) },
    ]);
    const signature = await pgp.sign({
      message: await pgp.createMessage({ binary: data }),
      signingKeys: privateKey,
      detached: true,
      format: "armored",
      config: cryptoConfig,
    });
    const extracted = await extractSigned(envelope(data, signature));
    assert.deepEqual(extracted.data, data);
    const noPreamble = envelope(data, signature);
    const clean = encode(
      new TextDecoder()
        .decode(noPreamble)
        .replace("Unsigned preamble must never render\r\n", ""),
    );
    assert.deepEqual((await extractSigned(clean)).data, data);
    const extraPart = encode(
      new TextDecoder()
        .decode(clean)
        .replace(
          "--signed-boundary--",
          "--signed-boundary\r\nContent-Type: text/plain\r\n\r\nInjected\r\n--signed-boundary--",
        ),
    );
    await assert.rejects(extractSigned(extraPart), { code: "invalid-mime" });

    const unknown = await reader.verifyDetached(
      extracted.data,
      extracted.signature,
      "signer@example.test",
    );
    assert.equal(unknown.signatures[0].status, "unknown-key");
    await reader.importPublic(key.publicKey);
    assert.equal(reader.locked, true);
    const untrusted = await reader.verifyDetached(
      extracted.data,
      extracted.signature,
      "signer@example.test",
    );
    assert.equal(untrusted.signatures[0].status, "valid");
    assert.equal(untrusted.signatures[0].trustedSender, false);
    await reader.trust("signer@example.test", key.fingerprint, key.fingerprint);
    const verified = await reader.verifyDetached(
      extracted.data,
      extracted.signature,
      "signer@example.test",
    );
    assert.equal(verified.signatures[0].trustedSender, true);
    const parsed = await parseLocalMime(verified.data);
    assert.equal(parsed.text, "Unicode Zażółć\nActual body");
    assert.equal(parsed.text.includes("preamble"), false);
    assert.deepEqual(
      new Uint8Array(parsed.attachments[0].content),
      new Uint8Array([0, 255, 13, 10]),
    );
    assert.equal(
      (
        await reader.verifyDetached(
          extracted.data,
          extracted.signature,
          "forged@example.test",
        )
      ).signatures[0].trustedSender,
      false,
    );
    const changed = new Uint8Array(data);
    changed[0] ^= 1;
    assert.equal(
      (
        await reader.verifyDetached(
          changed,
          extracted.signature,
          "signer@example.test",
        )
      ).signatures[0].status,
      "invalid",
    );
    await assert.rejects(
      extractSigned(envelope(data, signature).slice(0, -100)),
      { code: "invalid-mime" },
    );
    assert.equal(
      await extractSigned(encode("Content-Type: text/plain\r\n\r\nUnsigned")),
      null,
    );
  } finally {
    await signer.close();
    await reader.close();
  }
});
