// SPDX-License-Identifier: AGPL-3.0-only
import { openDB } from "../vendor/idb/index.js";
import { VaultError } from "./errors.mjs";
import { composeEntity } from "./mime.mjs";
const encoder = new TextEncoder(),
  decoder = new TextDecoder();
function encode(data) {
  let binary = "";
  for (let i = 0; i < data.length; i += 32768)
    binary += String.fromCharCode(...data.subarray(i, i + 32768));
  return btoa(binary);
}
function decode(value) {
  if (typeof value !== "string" || value.length > 2 << 20)
    throw new VaultError("invalid-draft");
  try {
    return Uint8Array.from(atob(value), (char) => char.charCodeAt(0));
  } catch {
    throw new VaultError("invalid-draft");
  }
}
function validate(value) {
  if (
    !value ||
    typeof value !== "object" ||
    ["to", "cc", "bcc"].some(
      (key) => typeof value[key] !== "string" || value[key].length > 8192,
    ) ||
    typeof value.subject !== "string" ||
    value.subject.length > 200 ||
    /[\r\n\0]/.test(value.subject) ||
    !Array.isArray(value.attachments) ||
    !value.reply ||
    typeof value.reply.folder !== "string" ||
    value.reply.folder.length > 1024 ||
    ![value.reply.uid, value.reply.validity].every(
      (n) => Number.isSafeInteger(n) && n >= 0 && n <= 4294967295,
    ) ||
    !/^[A-Za-z0-9_-]{26,64}$/.test(value.requestId)
  )
    throw new VaultError("invalid-draft");
  if (encoder.encode(value.subject).length > 200)
    throw new VaultError("subject-limit");
  composeEntity(value.body, value.attachments);
}
export class LocalDrafts {
  #db;
  constructor(userID) {
    if (!/^[A-Za-z0-9_-]{16,128}$/.test(userID))
      throw new VaultError("invalid-user");
    this.#db = openDB(`mailspace-encrypted-drafts-v1-${userID}`, 1, {
      upgrade(db) {
        db.createObjectStore("drafts", { keyPath: "id" });
      },
    });
  }
  async list(account) {
    return (await (await this.#db).getAll("drafts"))
      .filter((record) => record.account === account)
      .map(({ id, updatedAt, fingerprint }) => ({ id, updatedAt, fingerprint }))
      .sort((a, b) => b.updatedAt - a.updatedAt);
  }
  async save(vault, account, from, value, id = crypto.randomUUID()) {
    validate(value);
    if (
      typeof account !== "string" ||
      !/^[A-Za-z0-9_-]{16,128}$/.test(account) ||
      !/^[a-f0-9-]{36}$/.test(id)
    )
      throw new VaultError("invalid-draft");
    const payload = {
      version: 1,
      account,
      from,
      to: value.to,
      cc: value.cc,
      bcc: value.bcc,
      subject: value.subject,
      body: value.body,
      reply: value.reply,
      requestId: value.requestId,
      attachments: value.attachments.map((file) => ({
        name: file.name,
        base64: encode(file.data),
      })),
    };
    const fingerprint = vault.fingerprint;
    const ciphertext = await vault.encryptSigned(
      encoder.encode(JSON.stringify(payload)),
      from,
      [from],
    );
    const db = await this.#db,
      tx = db.transaction("drafts", "readwrite");
    try {
      const all = await tx.store.getAll(),
        existing = all.find((record) => record.id === id);
      if (existing && existing.account !== account)
        throw new VaultError("invalid-draft");
      if (
        (!existing && all.length >= 20) ||
        ciphertext.length +
          all
            .filter((record) => record.id !== id)
            .reduce((sum, record) => sum + record.ciphertext.length, 0) >
          16 << 20
      )
        throw new VaultError("draft-limit");
      await tx.store.put({
        id,
        account,
        fingerprint,
        ciphertext,
        updatedAt: Date.now(),
      });
      await tx.done;
    } catch (error) {
      try {
        tx.abort();
      } catch {}
      await tx.done.catch(() => {});
      throw error;
    }
    return id;
  }
  async load(vault, account, from, id) {
    const record = await (await this.#db).get("drafts", id);
    if (!record || record.account !== account)
      throw new VaultError("missing-draft");
    const result = await vault.decryptSigned(record.ciphertext, from);
    if (
      !result.signatures.some(
        (signature) =>
          signature.status === "valid" &&
          signature.fingerprint === record.fingerprint,
      )
    )
      throw new VaultError("invalid-draft");
    let payload;
    try {
      payload = JSON.parse(decoder.decode(result.data));
    } catch {
      throw new VaultError("invalid-draft");
    }
    if (
      payload?.version !== 1 ||
      payload.account !== account ||
      payload.from !== from ||
      !Array.isArray(payload.attachments) ||
      payload.attachments.length > 10
    )
      throw new VaultError("invalid-draft");
    const value = {
      to: payload.to,
      cc: payload.cc,
      bcc: payload.bcc,
      subject: payload.subject,
      body: payload.body,
      reply: payload.reply,
      requestId: payload.requestId,
      attachments: payload.attachments.map((file) => ({
        name: file.name,
        data: decode(file.base64),
      })),
    };
    validate(value);
    return value;
  }
  async remove(account, id) {
    const db = await this.#db,
      tx = db.transaction("drafts", "readwrite");
    const record = await tx.store.get(id);
    if (record && record.account === account) await tx.store.delete(id);
    await tx.done;
  }
  async close() {
    (await this.#db).close();
  }
}
