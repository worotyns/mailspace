// SPDX-License-Identifier: AGPL-3.0-only
import "fake-indexeddb/auto";
import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, chmod, writeFile, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import * as pgp from "../../internal/webui/assets/vendor/openpgp/openpgp.min.mjs";
import { VaultStore } from "../../internal/webui/assets/crypto/store.mjs";
import {
  LocalVault,
  cryptoConfig,
} from "../../internal/webui/assets/crypto/vault.mjs";

import {
  composeEntity,
  encryptedEntity,
  extractEncrypted,
} from "../../internal/webui/assets/crypto/mime.mjs";

const version = spawnSync("gpg", ["--version"], { encoding: "utf8" });
test(
  "v4 protected keys and signed encryption interoperate with GnuPG",
  { timeout: 60_000 },
  async (t) => {
    if (version.error || version.status !== 0) {
      if (process.env.CI)
        assert.fail("GnuPG is required for the CI interoperability gate");
      t.skip(
        "GnuPG unavailable; interoperability gate must be run before release",
      );
      return;
    }
    t.diagnostic(version.stdout.split("\n")[0]);
    const home = await mkdtemp(join(tmpdir(), "mailspace-gpg-"));
    await chmod(home, 0o700);
    const password = "Synthetic GnuPG interoperability passphrase";
    const vault = new LocalVault(
      new VaultStore("SYNTHETIC_GNUPG_INTEROP_USER"),
    );
    const gpg = (args) => {
      const result = spawnSync(
        "gpg",
        [
          "--homedir",
          home,
          "--batch",
          "--yes",
          "--pinentry-mode",
          "loopback",
          "--passphrase-fd",
          "0",
          ...args,
        ],
        {
          input: password + "\n",
          encoding: "utf8",
          timeout: 20_000,
          env: { ...process.env, GNUPGHOME: home },
        },
      );
      assert.equal(result.status, 0, result.stderr || "GnuPG operation failed");
      return result.stdout;
    };
    try {
      const generated = await vault.generate(
        "OpenPGP Fixture",
        "interop@example.test",
        password,
      );
      const keyFile = join(home, "protected.asc");
      await writeFile(
        keyFile,
        await vault.exportPrivate(generated.fingerprint),
        { mode: 0o600 },
      );
      gpg(["--import", keyFile]);
      assert.ok(
        gpg(["--with-colons", "--list-secret-keys"]).includes(
          generated.fingerprint,
        ),
      );
      const privateKey = await pgp.decryptKey({
        privateKey: await pgp.readPrivateKey({
          armoredKey: await vault.exportPrivate(generated.fingerprint),
        }),
        passphrase: password,
        config: cryptoConfig,
      });
      const publicKey = privateKey.toPublic();
      const plaintext = new TextEncoder().encode(
        "Synthetic Unicode mail: Zażółć gęślą\r\n",
      );
      const armored = await pgp.encrypt({
        message: await pgp.createMessage({ binary: plaintext }),
        encryptionKeys: [publicKey],
        signingKeys: [privateKey],
        wildcard: true,
        format: "armored",
        config: cryptoConfig,
      });
      const encryptedFile = join(home, "encrypted.asc");
      await writeFile(encryptedFile, armored, { mode: 0o600 });
      assert.equal(
        gpg(["--decrypt", encryptedFile]),
        new TextDecoder().decode(plaintext),
      );
      const plainFile = join(home, "plain.txt");
      const gpgEncrypted = join(home, "gpg-encrypted.asc");
      await writeFile(plainFile, plaintext, { mode: 0o600 });
      // Fixture-only trust: no production UI or transport uses trust-model always.
      gpg([
        "--trust-model",
        "always",
        "--armor",
        "--local-user",
        generated.fingerprint,
        "--recipient",
        generated.fingerprint,
        "--output",
        gpgEncrypted,
        "--sign",
        "--encrypt",
        plainFile,
      ]);
      const decrypted = await pgp.decrypt({
        message: await pgp.readMessage({
          armoredMessage: await readFile(gpgEncrypted, "utf8"),
        }),
        decryptionKeys: [privateKey],
        verificationKeys: [publicKey],
        format: "binary",
        config: cryptoConfig,
      });
      assert.deepEqual(decrypted.data, plaintext);
      assert.equal(decrypted.signatures.length, 1);
      await decrypted.signatures[0].verified;
      await vault.trust(
        "interop@example.test",
        generated.fingerprint,
        generated.fingerprint,
      );
      await vault.unlock(generated.fingerprint, password);
      const mimePlain = composeEntity("MIME Unicode: Zażółć gęślą\nLine two", [
        {
          name: "GnuPG attachment.bin",
          data: new Uint8Array([0, 255, 13, 10, 42]),
        },
      ]);
      const appCiphertext = await vault.encryptSigned(
        mimePlain,
        "interop@example.test",
        ["interop@example.test"],
      );
      const transported = await extractEncrypted(
        new TextEncoder().encode(encryptedEntity(appCiphertext)),
      );
      await writeFile(encryptedFile, transported, { mode: 0o600 });
      assert.equal(
        gpg(["--decrypt", encryptedFile]),
        new TextDecoder().decode(mimePlain),
      );
      const fromGnuPG = await vault.decryptSigned(
        await readFile(gpgEncrypted, "utf8"),
        "interop@example.test",
      );
      assert.deepEqual(fromGnuPG.data, plaintext);
      assert.equal(fromGnuPG.signatures[0].trustedSender, true);
      gpg([
        "--quick-generate-key",
        "GnuPG Fixture <gpg@example.test>",
        "rsa2048",
        "sign,cert,encr",
        "1d",
      ]);
      const legacySecret = gpg([
        "--armor",
        "--export-secret-keys",
        "gpg@example.test",
      ]);
      const imported = await vault.importPrivate(legacySecret, password);
      await vault.unlock(imported, password);
      assert.equal(vault.fingerprint, imported);
      assert.equal(
        (
          await pgp.readPrivateKey({
            armoredKey: await vault.exportPrivate(imported),
          })
        ).isDecrypted(),
        false,
      );
    } finally {
      await vault.close();
      spawnSync("gpgconf", ["--homedir", home, "--kill", "all"], {
        env: { ...process.env, GNUPGHOME: home },
        timeout: 5000,
      });
      await rm(home, { recursive: true, force: true });
    }
  },
);
