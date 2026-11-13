// SPDX-License-Identifier: AGPL-3.0-only
import "fake-indexeddb/auto";
import test from "node:test";
import assert from "node:assert/strict";
import { openDB } from "../../internal/webui/assets/vendor/idb/index.js";
import { VaultStore } from "../../internal/webui/assets/crypto/store.mjs";
import { LocalVault } from "../../internal/webui/assets/crypto/vault.mjs";
import { LocalDrafts } from "../../internal/webui/assets/crypto/drafts.mjs";
const user = "SYNTHETIC_LOCAL_DRAFT_USER",
  account = "SYNTHETIC_DRAFT_ACCOUNT",
  from = "draft@example.test",
  password = "Synthetic local draft passphrase";
const value = () => ({
  to: "missing-key@example.test",
  cc: "",
  bcc: "hidden@example.test",
  subject: "PRIVATE_SUBJECT_CANARY",
  body: "PRIVATE_DRAFT_BODY_CANARY",
  reply: { folder: "INBOX", uid: 7, validity: 42 },
  requestId: "SYNTHETIC_ORIGINAL_SEND_NONCE_12345",
  attachments: [
    {
      name: "PRIVATE_FILENAME_CANARY.bin",
      data: new Uint8Array([0, 255, 13, 10]),
    },
  ],
});
globalThis.fetch = () => {
  throw new Error("Draft module attempted network access");
};
test("draft persistence is ciphertext-only and user/account isolated; reopening requires unlock", async () => {
  const vault = new LocalVault(new VaultStore(user));
  let drafts = new LocalDrafts(user);
  const generated = await vault.generate("Synthetic draft", from, password);
  await vault.trust(from, generated.fingerprint, generated.fingerprint);
  await vault.unlock(generated.fingerprint, password);
  try {
    const id = await drafts.save(vault, account, from, value());
    const rawDB = await openDB(`mailspace-encrypted-drafts-v1-${user}`, 1);
    const records = await rawDB.getAll("drafts");
    rawDB.close();
    assert.equal(records.length, 1);
    const persisted = JSON.stringify(records);
    for (const canary of [
      "PRIVATE_SUBJECT_CANARY",
      "PRIVATE_DRAFT_BODY_CANARY",
      "PRIVATE_FILENAME_CANARY.bin",
      password,
      "missing-key@example.test",
      "hidden@example.test",
    ])
      assert.equal(persisted.includes(canary), false);
    assert.deepEqual(
      Object.keys(records[0]).sort(),
      ["id", "account", "fingerprint", "ciphertext", "updatedAt"].sort(),
    );
    await drafts.close();
    drafts = new LocalDrafts(user);
    vault.lock();
    assert.equal((await drafts.list(account)).length, 1);
    await assert.rejects(drafts.load(vault, account, from, id), {
      code: "locked",
    });
    await vault.unlock(generated.fingerprint, password);
    assert.deepEqual(await drafts.load(vault, account, from, id), value());
    assert.equal((await drafts.list("SYNTHETIC_OTHER_ACCOUNT")).length, 0);
    await assert.rejects(
      drafts.load(vault, "SYNTHETIC_OTHER_ACCOUNT", from, id),
      { code: "missing-draft" },
    );
    await assert.rejects(
      drafts.load(vault, account, "different@example.test", id),
      { code: "invalid-draft" },
    );
    const other = new LocalDrafts("SYNTHETIC_OTHER_DRAFT_USER");
    assert.equal((await other.list(account)).length, 0);
    await other.close();
    await drafts.remove("SYNTHETIC_OTHER_ACCOUNT", id);
    assert.equal((await drafts.list(account)).length, 1);
    const foreignID = await drafts.save(
      vault,
      "SYNTHETIC_OTHER_ACCOUNT",
      from,
      value(),
    );
    const tamperDB = await openDB(`mailspace-encrypted-drafts-v1-${user}`, 1);
    const transplanted = await tamperDB.get("drafts", foreignID);
    transplanted.account = account;
    await tamperDB.put("drafts", transplanted);
    tamperDB.close();
    await assert.rejects(drafts.load(vault, account, from, foreignID), {
      code: "invalid-draft",
    });
    await drafts.remove(account, foreignID);
    await drafts.remove(account, id);
    assert.equal((await drafts.list(account)).length, 0);
  } finally {
    await drafts.close();
    await vault.close();
  }
});
test("draft capacity and payload bounds fail closed without overwriting saved data", async () => {
  const owner = "SYNTHETIC_DRAFT_CAPACITY_USER",
    vault = new LocalVault(new VaultStore(owner)),
    drafts = new LocalDrafts(owner);
  const generated = await vault.generate("Synthetic draft", from, password);
  await vault.trust(from, generated.fingerprint, generated.fingerprint);
  await vault.unlock(generated.fingerprint, password);
  try {
    let first;
    for (let n = 0; n < 20; n++) {
      const id = await drafts.save(vault, account, from, value());
      first ||= id;
    }
    await assert.rejects(drafts.save(vault, account, from, value()), {
      code: "draft-limit",
    });
    assert.equal((await drafts.list(account)).length, 20);
    const updated = { ...value(), body: "Updated private body" };
    assert.equal(
      await drafts.save(vault, account, from, updated, first),
      first,
    );
    assert.equal(
      (await drafts.load(vault, account, from, first)).body,
      updated.body,
    );
    await assert.rejects(
      drafts.save(vault, "SYNTHETIC_OTHER_ACCOUNT", from, value(), first),
      { code: "invalid-draft" },
    );
    await assert.rejects(
      drafts.save(vault, account, from, { ...value(), requestId: "bad" }),
      { code: "invalid-draft" },
    );
    await assert.rejects(
      drafts.save(vault, account, from, {
        ...value(),
        body: "x".repeat((256 << 10) + 1),
      }),
      { code: "message-limit" },
    );
  } finally {
    await drafts.close();
    await vault.close();
  }
});
