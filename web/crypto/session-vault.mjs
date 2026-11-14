// SPDX-License-Identifier: AGPL-3.0-only
import { LocalVault, VaultError } from "./vault.mjs";
import { VaultStore } from "./store.mjs";

// A passive document owner prevents last-port gaps during HTMX replacement.
// It sends no crypto/activity RPC or network request; pagehide only disconnects.
let documentKeeper = null;
function keepSharedWorker(url, options, userID) {
  if (
    documentKeeper ||
    typeof window === "undefined" ||
    typeof window.addEventListener !== "function"
  )
    return;
  documentKeeper = new SharedWorker(url, options);
  documentKeeper.port.start?.();
  window.addEventListener(
    "pagehide",
    () => {
      try {
        documentKeeper?.port.postMessage({
          id: 1,
          userID,
          op: "disconnect",
          args: [],
        });
      } catch {}
      documentKeeper?.port.close?.();
      documentKeeper = null;
    },
    { once: true },
  );
}

class SessionVault extends EventTarget {
  #port;
  #worker;
  #pending = new Map();
  #id = 0;
  #closed = false;
  #epoch = 0;
  #locking = false;
  #state = { locked: true, fingerprint: null, expiresAt: 0 };
  constructor(userID) {
    super();
    if (!/^[A-Za-z0-9_-]{16,128}$/.test(userID))
      throw new VaultError("invalid-user");
    this.userID = userID;
    const url = new URL("./session-worker.mjs", import.meta.url);
    try {
      if (typeof SharedWorker !== "function") throw new Error("unavailable");
      const options = {
        type: "module",
        name: "mailspace-vault-v2",
        extendedLifetime: true,
      };
      keepSharedWorker(url, options, userID);
      this.#worker = new SharedWorker(url, options);
      this.#port = this.#worker.port;
      this.sessionMode = "shared";
      this.shared = true;
    } catch {
      this.#worker = new Worker(url, {
        type: "module",
        name: "mailspace-vault-v2",
      });
      this.#port = this.#worker;
      this.sessionMode = "dedicated";
      this.shared = false;
    }
    this.#port.addEventListener("message", (event) => {
      const message = event.data;
      if (message?.event === "state") {
        if (!this.#locking || message.state?.locked !== false)
          this.#update(message.state);
        return;
      }
      const pending = this.#pending.get(message?.id);
      if (!pending) return;
      this.#pending.delete(message.id);
      if (pending.epoch !== this.#epoch) {
        clearTimeout(pending.timer);
        pending.reject(new VaultError("locked"));
        return;
      }
      if (message.state && (!this.#locking || message.state.locked !== false))
        this.#update(message.state);
      clearTimeout(pending.timer);
      if (message.error) pending.reject(new VaultError(message.error));
      else pending.resolve(message.result);
    });
    this.#worker.addEventListener("error", () => this.#fail());
    this.#port.addEventListener("messageerror", () => this.#fail());
    this.#port.start?.();
  }
  #update(value) {
    const wasLocked = this.#state.locked;
    this.#state = {
      locked: value.locked !== false,
      fingerprint:
        typeof value.fingerprint === "string" ? value.fingerprint : null,
      expiresAt: Number(value.expiresAt) || 0,
    };
    this.dispatchEvent(new Event("statechange"));
    if (!wasLocked && this.#state.locked) this.dispatchEvent(new Event("lock"));
  }
  #fail() {
    this.#epoch++;
    this.#update({ locked: true });
    for (const pending of this.#pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(new VaultError("locked"));
    }
    this.#pending.clear();
  }
  get locked() {
    return this.#state.locked || Date.now() >= this.#state.expiresAt;
  }
  get fingerprint() {
    return this.locked ? null : this.#state.fingerprint;
  }
  onStateChange(callback) {
    const listener = () => callback({ ...this.#state });
    this.addEventListener("statechange", listener);
    return () => this.removeEventListener("statechange", listener);
  }
  #call(op, args = []) {
    if (this.#closed || (this.#locking && op !== "lock"))
      return Promise.reject(new VaultError("locked"));
    const id = ++this.#id;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.#pending.delete(id);
        this.lock();
        reject(new VaultError("locked"));
      }, 60000);
      this.#pending.set(id, { resolve, reject, timer, epoch: this.#epoch });
      try {
        this.#port.postMessage({ id, userID: this.userID, op, args });
      } catch {
        clearTimeout(timer);
        this.#pending.delete(id);
        reject(new VaultError("locked"));
      }
    });
  }
  list() {
    return this.#call("list");
  }
  unlock(...args) {
    return this.#call("unlock", args);
  }
  encryptSigned(...args) {
    return this.#call("encryptSigned", args);
  }
  decryptSigned(...args) {
    return this.#call("decryptSigned", args);
  }
  signDetached(...args) {
    return this.#call("signDetached", args);
  }
  verifyDetached(...args) {
    return this.#call("verifyDetached", args);
  }
  touch() {
    return this.#call("touch");
  }
  lock() {
    if (this.#locking) return;
    this.#locking = true;
    this.#fail();
    void this.#call("lock")
      .catch(() => {})
      .finally(() => {
        this.#locking = false;
      });
  }
  async close() {
    if (this.#closed) return;
    try {
      this.#port.postMessage({
        id: ++this.#id,
        userID: this.userID,
        op: "disconnect",
        args: [],
      });
    } catch {}
    this.#closed = true;
    this.#fail();
    this.#port.close?.();
    if (!this.shared) this.#worker.terminate();
  }
}
export function createSessionVault(userID) {
  if (typeof Worker === "function" || typeof SharedWorker === "function") {
    try {
      return new SessionVault(userID);
    } catch {
      /* Browser policy may deny both worker constructors. */
    }
  }
  const vault = new LocalVault(new VaultStore(userID));
  vault.sessionMode = "page";
  vault.shared = false;
  return vault;
}
