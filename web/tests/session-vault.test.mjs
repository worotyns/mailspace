// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { createSessionVault } from "../../internal/webui/assets/crypto/session-vault.mjs";

test("client lock rejects pending plaintext and ignores stale worker unlock messages", async () => {
  const previous = {
    Worker: globalThis.Worker,
    SharedWorker: globalThis.SharedWorker,
  };
  let port;
  class Port extends EventTarget {
    sent = [];
    postMessage(value) {
      this.sent.push(value);
    }
    start() {}
    close() {}
    emit(data) {
      this.dispatchEvent(new MessageEvent("message", { data }));
    }
  }
  class Shared extends EventTarget {
    constructor(url, options) {
      super();
      assert.equal(url.pathname.endsWith("/crypto/session-worker.mjs"), true);
      assert.equal(options.type, "module");
      assert.equal(options.extendedLifetime, true);
      port = new Port();
      this.port = port;
    }
  }
  try {
    globalThis.SharedWorker = Shared;
    globalThis.Worker = class {};
    const vault = createSessionVault("SYNTHETIC_PROXY_USER_123");
    const list = vault.list();
    let message = port.sent.at(-1);
    port.emit({
      id: message.id,
      result: { keys: [] },
      state: { locked: true, expiresAt: Date.now() + 300000 },
    });
    await list;
    const unlock = vault.unlock("A".repeat(40), "PRIVATE_PASSPHRASE_CANARY");
    message = port.sent.at(-1);
    port.emit({
      id: message.id,
      result: "A".repeat(40),
      state: {
        locked: false,
        fingerprint: "A".repeat(40),
        expiresAt: Date.now() + 300000,
      },
    });
    await unlock;
    assert.equal(vault.locked, false);
    const decrypt = vault.decryptSigned(
        new Uint8Array([1]),
        "sender@example.test",
      ),
      pending = port.sent.at(-1);
    const rejected = assert.rejects(
      decrypt,
      (error) => error.code === "locked",
    );
    vault.lock();
    await rejected;
    port.emit({
      id: pending.id,
      result: { data: "PRIVATE_DECRYPTED_CANARY" },
      state: {
        locked: false,
        fingerprint: "A".repeat(40),
        expiresAt: Date.now() + 300000,
      },
    });
    port.emit({
      event: "state",
      state: {
        locked: false,
        fingerprint: "A".repeat(40),
        expiresAt: Date.now() + 300000,
      },
    });
    assert.equal(vault.locked, true);
    assert.equal(vault.fingerprint, null);
    const lock = port.sent.at(-1);
    assert.equal(lock.op, "lock");
    port.emit({ id: lock.id, result: null, state: { locked: true } });
    await vault.close();
    assert.equal(port.sent.at(-1).op, "disconnect");
  } finally {
    globalThis.Worker = previous.Worker;
    globalThis.SharedWorker = previous.SharedWorker;
  }
});

test("worker constructor denial preserves protected page-local crypto fallback", async () => {
  const previous = {
    Worker: globalThis.Worker,
    SharedWorker: globalThis.SharedWorker,
  };
  await import("fake-indexeddb/auto");
  try {
    globalThis.SharedWorker = class {
      constructor() {
        throw new Error("denied");
      }
    };
    globalThis.Worker = class {
      constructor() {
        throw new Error("denied");
      }
    };
    const vault = createSessionVault("SYNTHETIC_DENIED_WORKER_123");
    assert.equal(vault.sessionMode, "page");
    assert.equal(vault.shared, false);
    assert.equal(vault.locked, true);
    await vault.close();
  } finally {
    globalThis.Worker = previous.Worker;
    globalThis.SharedWorker = previous.SharedWorker;
  }
});
test("document keeper is passive and closes only on pagehide", async () => {
  const previous = {
    Worker: globalThis.Worker,
    SharedWorker: globalThis.SharedWorker,
    window: globalThis.window,
  };
  const ports = [];
  class Port extends EventTarget {
    sent = [];
    closed = false;
    start() {}
    postMessage(value) {
      this.sent.push(value);
    }
    close() {
      this.closed = true;
    }
  }
  class Shared extends EventTarget {
    constructor() {
      super();
      this.port = new Port();
      ports.push(this.port);
    }
  }
  try {
    globalThis.window = new EventTarget();
    globalThis.SharedWorker = Shared;
    globalThis.Worker = class {};
    const a = createSessionVault("SYNTHETIC_KEEPER_USER_123"),
      b = createSessionVault("SYNTHETIC_KEEPER_USER_123");
    assert.equal(ports.length, 3);
    assert.equal(ports[0].sent.length, 0);
    await a.close();
    await b.close();
    assert.equal(ports[0].closed, false);
    assert.equal(ports[0].sent.length, 0);
    globalThis.window.dispatchEvent(new Event("pagehide"));
    assert.equal(ports[0].closed, true);
    assert.deepEqual(
      ports[0].sent.map((message) => message.op),
      ["disconnect"],
    );
  } finally {
    globalThis.Worker = previous.Worker;
    globalThis.SharedWorker = previous.SharedWorker;
    globalThis.window = previous.window;
  }
});

test("asynchronous worker loading errors fail immediately without silently changing vault mode", async () => {
  const saved = {
    SharedWorker: globalThis.SharedWorker,
    Worker: globalThis.Worker,
  };
  let worker;
  class Port extends EventTarget {
    postMessage() {}
    start() {}
    close() {}
  }
  class Shared extends EventTarget {
    constructor() {
      super();
      this.port = new Port();
      worker = this;
    }
  }
  try {
    globalThis.SharedWorker = Shared;
    globalThis.Worker = class {
      constructor() {
        throw new Error("must not silently fall back");
      }
    };
    const vault = createSessionVault("SYNTHETIC_ASYNC_WORKER_USER");
    const pending = vault.list();
    const rejected = assert.rejects(
      pending,
      (e) => e.code === "worker-unavailable",
    );
    worker.dispatchEvent(new Event("error"));
    await rejected;
    await assert.rejects(vault.list(), (e) => e.code === "worker-unavailable");
    assert.equal(vault.sessionMode, "shared");
    assert.equal(vault.locked, true);
    await vault.close();
  } finally {
    globalThis.SharedWorker = saved.SharedWorker;
    globalThis.Worker = saved.Worker;
  }
});
