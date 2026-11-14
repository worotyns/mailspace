// SPDX-License-Identifier: AGPL-3.0-only
import { LocalVault, VaultError } from "./vault.mjs";
import { VaultStore } from "./store.mjs";

// A passive document owner prevents last-port gaps during HTMX replacement.
// It sends no crypto/activity RPC or network request; pagehide only disconnects.
let documentKeeper = null;
function retireKeeper(keeper) {
  if (!keeper) return;
  try {
    keeper.worker.port.postMessage({
      id: 1,
      userID: keeper.userID,
      op: "disconnect",
      args: [],
    });
  } catch {}
  keeper.worker.port.close?.();
  if (documentKeeper === keeper) documentKeeper = null;
}
function keepSharedWorker(url, options, userID) {
  if (
    documentKeeper ||
    typeof window === "undefined" ||
    typeof window.addEventListener !== "function"
  )
    return;
  const keeper = {
    worker: new SharedWorker(url, options),
    url: url.href,
    userID,
  };
  documentKeeper = keeper;
  keeper.worker.port.start?.();
  window.addEventListener("pagehide", () => retireKeeper(keeper), {
    once: true,
  });
}

class SessionVault extends EventTarget {
  #port;
  #worker;
  #pending = new Map();
  #id = 0;
  #closed = false;
  #epoch = 0;
  #locking = false;
  #unavailable = false;
  #responseSeen = false;
  #nonListDispatched = false;
  #startupFallbackUsed = false;
  #transportGeneration = 0;
  #removeListeners = () => {};
  #url;
  #keeper;
  #state = { locked: true, fingerprint: null, expiresAt: 0 };
  constructor(userID) {
    super();
    if (!/^[A-Za-z0-9_-]{16,128}$/.test(userID))
      throw new VaultError("invalid-user");
    this.userID = userID;
    const url = (this.#url = new URL("./session-worker.mjs", import.meta.url));
    try {
      if (typeof SharedWorker !== "function") throw new Error("unavailable");
      const options = {
        type: "module",
        name: "mailspace-vault-v2",
        extendedLifetime: true,
      };
      keepSharedWorker(url, options, userID);
      this.#keeper = documentKeeper;
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
    this.#listen();
  }
  #listen() {
    const port = this.#port,
      worker = this.#worker;
    const generation = ++this.#transportGeneration;
    const current = () =>
      !this.#closed && generation === this.#transportGeneration;
    const message = (event) => {
      if (!current()) return;
      // Any response proves startup happened; RPC/auth errors never fail over.
      this.#responseSeen = true;
      const value = event.data;
      if (value?.event === "state") {
        if (!this.#locking || value.state?.locked !== false)
          this.#update(value.state);
        return;
      }
      const pending = this.#pending.get(value?.id);
      if (!pending) return;
      this.#pending.delete(value.id);
      clearTimeout(pending.timer);
      if (pending.epoch !== this.#epoch) {
        pending.reject(new VaultError("locked"));
        return;
      }
      if (value.state && (!this.#locking || value.state.locked !== false))
        this.#update(value.state);
      if (value.error) pending.reject(new VaultError(value.error));
      else pending.resolve(value.result);
    };
    const unavailable = () => {
      if (!current()) return;
      this.#unavailable = true;
      this.#removeListeners();
      this.#transportGeneration++;
      this.#fail("worker-unavailable");
    };
    const error = () => {
      if (!current()) return;
      if (this.#fallbackStartup()) return;
      unavailable();
    };
    port.addEventListener("message", message);
    port.addEventListener("messageerror", unavailable);
    worker.addEventListener("error", error);
    this.#removeListeners = () => {
      port.removeEventListener("message", message);
      port.removeEventListener("messageerror", unavailable);
      worker.removeEventListener("error", error);
    };
    port.start?.();
  }
  #fallbackStartup() {
    if (
      !this.shared ||
      this.#startupFallbackUsed ||
      this.#responseSeen ||
      this.#nonListDispatched ||
      this.#closed ||
      this.#locking ||
      this.#unavailable ||
      [...this.#pending.values()].some((p) => p.op !== "list")
    )
      return false;
    this.#startupFallbackUsed = true;
    this.#removeListeners();
    this.#transportGeneration++;
    this.#port.close?.();
    retireKeeper(this.#keeper);
    try {
      // Only metadata listing can be retried. The fresh worker uses the same
      // authenticated kernel; private arguments are never retained or replayed.
      const worker = new Worker(this.#url, {
        type: "module",
        name: "mailspace-vault-v2",
      });
      this.#worker = this.#port = worker;
      this.sessionMode = "dedicated";
      this.shared = false;
      this.#listen();
      for (const [id] of this.#pending)
        worker.postMessage({ id, userID: this.userID, op: "list", args: [] });
      return true;
    } catch {
      this.#unavailable = true;
      this.#removeListeners();
      this.#transportGeneration++;
      this.#fail("worker-unavailable");
      return true;
    }
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
  #fail(code = "locked") {
    this.#epoch++;
    this.#update({ locked: true });
    for (const pending of this.#pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(new VaultError(code));
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
    if (this.#unavailable)
      return Promise.reject(new VaultError("worker-unavailable"));
    if (this.#closed || (this.#locking && op !== "lock"))
      return Promise.reject(new VaultError("locked"));
    const id = ++this.#id;
    return new Promise((resolve, reject) => {
      const pending = { resolve, reject, op, epoch: this.#epoch };
      pending.timer = setTimeout(
        () => {
          if (this.#pending.get(id) !== pending) return;
          this.#pending.delete(id);
          this.lock();
          reject(new VaultError("locked"));
        },
        ["list", "touch"].includes(op) ? 8000 : 60000,
      );
      this.#pending.set(id, pending);
      try {
        if (op !== "list") this.#nonListDispatched = true;
        this.#port.postMessage({ id, userID: this.userID, op, args });
      } catch {
        clearTimeout(pending.timer);
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
    this.#removeListeners();
    this.#transportGeneration++;
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
