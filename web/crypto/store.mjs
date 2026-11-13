// SPDX-License-Identifier: AGPL-3.0-only
import { openDB } from "../vendor/idb/index.js";
import { VaultError } from "./errors.mjs";

// The opaque local user id partitions UI state. The browser origin can still
// access IndexedDB: cryptographic at-rest protection comes from protected keys.
export class VaultStore {
  #db;
  constructor(userID) {
    if (!/^[A-Za-z0-9_-]{16,128}$/.test(userID))
      throw new VaultError("invalid-user");
    this.name = `mailspace-key-vault-v1-${userID}`;
    this.#db = openDB(this.name, 1, {
      upgrade(db) {
        db.createObjectStore("privateKeys", { keyPath: "fingerprint" });
        db.createObjectStore("publicKeys", { keyPath: "fingerprint" });
        db.createObjectStore("bindings", { keyPath: "email" });
      },
    });
  }
  async privateKeys() {
    return (await this.#db).getAll("privateKeys");
  }
  async publicKeys() {
    return (await this.#db).getAll("publicKeys");
  }
  async bindings() {
    return (await this.#db).getAll("bindings");
  }
  async privateKey(fingerprint) {
    return (await this.#db).get("privateKeys", fingerprint);
  }
  async publicKey(fingerprint) {
    return (await this.#db).get("publicKeys", fingerprint);
  }
  async binding(email) {
    return (await this.#db).get("bindings", email);
  }
  async putPrivate(record, publicRecord) {
    const db = await this.#db;
    const tx = db.transaction(["privateKeys", "publicKeys"], "readwrite");
    try {
      const privateStore = tx.objectStore("privateKeys");
      const publicStore = tx.objectStore("publicKeys");
      if (
        !(await privateStore.get(record.fingerprint)) &&
        (await privateStore.count()) >= 20
      )
        throw new VaultError("key-limit");
      if (
        !(await publicStore.get(record.fingerprint)) &&
        (await publicStore.count()) >= 200
      )
        throw new VaultError("key-limit");
      await privateStore.put(record);
      await publicStore.put(publicRecord);
      await tx.done;
    } catch (error) {
      try {
        tx.abort();
      } catch {}
      await tx.done.catch(() => {});
      throw error;
    }
  }
  async putPublic(record) {
    const db = await this.#db;
    const tx = db.transaction("publicKeys", "readwrite");
    try {
      if (
        !(await tx.store.get(record.fingerprint)) &&
        (await tx.store.count()) >= 200
      )
        throw new VaultError("key-limit");
      await tx.store.put(record);
      await tx.done;
    } catch (error) {
      try {
        tx.abort();
      } catch {}
      await tx.done.catch(() => {});
      throw error;
    }
  }
  async trust(email, fingerprint, replace = false) {
    const db = await this.#db;
    const tx = db.transaction(["bindings", "publicKeys"], "readwrite");
    try {
      if (!(await tx.objectStore("publicKeys").get(fingerprint)))
        throw new VaultError("missing-key");
      const bindings = tx.objectStore("bindings");
      const existing = await bindings.get(email);
      if (existing && existing.fingerprint !== fingerprint && !replace)
        throw new VaultError("key-changed");
      await bindings.put({
        email,
        fingerprint,
        verifiedAt: new Date().toISOString(),
      });
      await tx.done;
    } catch (error) {
      try {
        tx.abort();
      } catch {}
      await tx.done.catch(() => {});
      throw error;
    }
  }
  async untrust(email) {
    await (await this.#db).delete("bindings", email);
  }
  async close() {
    (await this.#db).close();
  }
}
