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

const password = "Synthetic key passphrase 123";
const noNetwork = () => {
  throw new Error("Key vault attempted network access");
};
globalThis.fetch = noNetwork;
let sequence = 0;
function setup() {
  const store = new VaultStore(`SYNTHETIC_TEST_USER_${++sequence}`);
  return { store, vault: new LocalVault(store) };
}
async function keyPair(email, extra = {}) {
  return pgp.generateKey({
    type: "ecc",
    curve: "curve25519Legacy",
    userIDs: [{ name: "Synthetic", email }],
    format: "armored",
    config: cryptoConfig,
    ...extra,
  });
}

test("generated secret keys stay protected across close/reopen, export and lock", async () => {
  const { store, vault } = setup();
  const generated = await vault.generate(
    "Alice",
    "alice@example.test",
    password,
  );
  assert.equal(vault.locked, true);
  assert.match(generated.revocationCertificate, /BEGIN PGP PUBLIC KEY BLOCK/);
  const records = await store.privateKeys();
  assert.equal(records.length, 1);
  assert.equal(JSON.stringify(records).includes(password), false);
  assert.equal("revocationCertificate" in records[0], false);
  const parsed = await pgp.readPrivateKey({
    armoredKey: records[0].armoredKey,
  });
  assert.equal(parsed.isDecrypted(), false);
  await assert.rejects(vault.unlock(generated.fingerprint, "wrong password"), {
    code: "bad-passphrase",
  });
  await vault.unlock(generated.fingerprint, password);
  assert.equal(vault.fingerprint, generated.fingerprint);
  assert.equal(JSON.stringify(vault), "{}");
  vault.lock();
  assert.equal(vault.locked, true);
  assert.equal(
    (
      await pgp.readPrivateKey({
        armoredKey: await vault.exportPrivate(generated.fingerprint),
      })
    ).isDecrypted(),
    false,
  );
  await vault.close();
  const reopenedStore = new VaultStore("SYNTHETIC_TEST_USER_1");
  const reopened = new LocalVault(reopenedStore);
  assert.equal(
    (await reopened.list()).keys[0].fingerprint,
    generated.fingerprint,
  );
  assert.equal(reopened.locked, true);
  await reopened.unlock(generated.fingerprint, password);
  await reopened.close();
});

test("unprotected import requires protection and encrypted import validates password", async () => {
  const keys = await keyPair("import@example.test");
  const { store, vault } = setup();
  await assert.rejects(vault.importPrivate(keys.privateKey, "short"), {
    code: "unsafe-passphrase",
  });
  assert.equal((await store.privateKeys()).length, 0);
  const fingerprint = await vault.importPrivate(keys.privateKey, password);
  assert.equal(vault.locked, true);
  const protectedArmor = await vault.exportPrivate(fingerprint);
  assert.equal(
    (await pgp.readPrivateKey({ armoredKey: protectedArmor })).isDecrypted(),
    false,
  );
  const other = setup();
  await assert.rejects(other.vault.importPrivate(protectedArmor, "incorrect"), {
    code: "bad-passphrase",
  });
  assert.equal((await other.store.privateKeys()).length, 0);
  await other.vault.importPrivate(protectedArmor, password);
  assert.equal(other.vault.locked, true);
  await assert.rejects(other.vault.importPublic(protectedArmor), {
    code: "private-key-in-public-import",
  });
  await vault.close();
  await other.vault.close();
});

test("locking while unlock is pending never revives an unlocked key", async () => {
  const { vault } = setup();
  const generated = await vault.generate(
    "Alice",
    "alice@example.test",
    password,
  );
  const pending = vault.unlock(generated.fingerprint, password);
  vault.lock();
  await assert.rejects(pending, { code: "locked" });
  assert.equal(vault.locked, true);
  await vault.close();
});

test("public key discovery is not trust; changed fingerprints require explicit replacement", async () => {
  const { store, vault } = setup();
  const first = await keyPair("recipient@example.test");
  const fingerprint = await vault.importPublic(first.publicKey);
  assert.equal(await store.binding("recipient@example.test"), undefined);
  await assert.rejects(
    vault.trust("recipient@example.test", fingerprint, "0".repeat(40)),
    { code: "fingerprint-mismatch" },
  );
  await assert.rejects(
    vault.trust("unrelated@example.test", fingerprint, fingerprint),
    { code: "unusable-key" },
  );
  await vault.trust("recipient@example.test", fingerprint, fingerprint);
  assert.equal(
    (await store.binding("recipient@example.test")).fingerprint,
    fingerprint,
  );
  const second = await keyPair("recipient@example.test");
  const replacement = await vault.importPublic(second.publicKey);
  await assert.rejects(
    vault.trust("recipient@example.test", replacement, replacement),
    { code: "key-changed" },
  );
  assert.equal(
    (await store.binding("recipient@example.test")).fingerprint,
    fingerprint,
  );
  await vault.trust("recipient@example.test", replacement, replacement, true);
  assert.equal(
    (await store.binding("recipient@example.test")).fingerprint,
    replacement,
  );
  await vault.untrust("recipient@example.test");
  assert.equal(await store.binding("recipient@example.test"), undefined);
  await vault.close();
});

test("expired and revoked public keys cannot acquire encryption trust", async () => {
  const { vault } = setup();
  const expired = await keyPair("expired@example.test", {
    date: new Date(Date.now() - 86400_000),
    keyExpirationTime: 60,
  });
  const oldFingerprint = await vault.importPublic(expired.publicKey);
  await assert.rejects(
    vault.trust("expired@example.test", oldFingerprint, oldFingerprint),
    { code: "unusable-key" },
  );
  const active = await keyPair("revoked@example.test");
  const privateKey = await pgp.readPrivateKey({
    armoredKey: active.privateKey,
  });
  const revoked = await pgp.revokeKey({ key: privateKey, format: "armored" });
  const revokedFingerprint = await vault.importPublic(revoked.publicKey);
  await assert.rejects(
    vault.trust("revoked@example.test", revokedFingerprint, revokedFingerprint),
    { code: "unusable-key" },
  );
  const list = await vault.list();
  assert.notEqual(
    list.keys.find((k) => k.fingerprint === oldFingerprint).status,
    "usable",
  );
  assert.equal(
    list.keys.find((k) => k.fingerprint === revokedFingerprint).status,
    "revoked",
  );
  await vault.close();
});

test("local accounts have separate vault namespaces", async () => {
  const first = setup();
  const other = setup();
  await first.vault.generate("Alice", "alice@example.test", password);
  assert.equal((await other.vault.list()).keys.length, 0);
  await first.vault.close();
  await other.vault.close();
});

test("mixed packet protection never stores an unprotected secret packet", async () => {
  const generated = await keyPair("mixed@example.test");
  const key = await pgp.readPrivateKey({ armoredKey: generated.privateKey });
  await key.keyPacket.encrypt(password, cryptoConfig);
  key.keyPacket.clearPrivateParams();
  assert.equal(key.keyPacket.isDecrypted(), false);
  assert.equal(key.isDecrypted(), true);
  const { vault, store } = setup();
  await assert.rejects(vault.importPrivate(key.armor(), password), {
    code: "mixed-key-protection",
  });
  assert.equal((await store.privateKeys()).length, 0);
  await vault.close();
});
