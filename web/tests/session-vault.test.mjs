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

function startupFixture({ keeper = false, denyDedicated = false } = {}) {
  const saved = {
    Worker: globalThis.Worker,
    SharedWorker: globalThis.SharedWorker,
    window: globalThis.window,
  };
  const shared = [],
    dedicated = [],
    pagehide = [];
  class Port extends EventTarget {
    sent = [];
    closed = false;
    terminated = false;
    postMessage(value) {
      this.sent.push(value);
    }
    start() {}
    close() {
      this.closed = true;
    }
    terminate() {
      this.terminated = true;
    }
    emit(value) {
      this.dispatchEvent(new MessageEvent("message", { data: value }));
    }
  }
  class Shared extends EventTarget {
    constructor() {
      super();
      this.port = new Port();
      shared.push(this);
    }
  }
  class Dedicated extends Port {
    constructor() {
      super();
      if (denyDedicated) throw new Error("unavailable");
      dedicated.push(this);
    }
  }
  globalThis.SharedWorker = Shared;
  globalThis.Worker = Dedicated;
  if (keeper)
    globalThis.window = {
      addEventListener(type, callback) {
        if (type === "pagehide") pagehide.push(callback);
      },
    };
  else delete globalThis.window;
  const vault = createSessionVault("SYNTHETIC_STARTUP_USER_123");
  return {
    vault,
    shared,
    dedicated,
    pagehide,
    async close() {
      await vault.close();
      for (const callback of pagehide) callback();
      for (const [name, value] of Object.entries(saved)) {
        if (value === undefined) delete globalThis[name];
        else globalThis[name] = value;
      }
    },
  };
}
test("only metadata listing recovers once into a fresh dedicated worker; old transport stays inert", async () => {
  const f = startupFixture();
  try {
    const pending = f.vault.list(),
      old = f.shared[0],
      request = old.port.sent.at(-1);
    old.dispatchEvent(new Event("error"));
    assert.equal(f.vault.sessionMode, "dedicated");
    assert.equal(f.vault.locked, true);
    assert.equal(old.port.closed, true);
    assert.deepEqual(f.dedicated[0].sent, [
      {
        id: request.id,
        userID: "SYNTHETIC_STARTUP_USER_123",
        op: "list",
        args: [],
      },
    ]);
    old.port.emit({
      id: request.id,
      result: { keys: ["stale"] },
      state: {
        locked: false,
        fingerprint: "A".repeat(40),
        expiresAt: Date.now() + 60000,
      },
    });
    old.dispatchEvent(new Event("error"));
    assert.equal(f.vault.locked, true);
    f.dedicated[0].emit({
      id: request.id,
      result: { keys: [], locked: true },
      state: { locked: true },
    });
    assert.deepEqual(await pending, { keys: [], locked: true });
    const next = f.vault.list(),
      rejected = assert.rejects(next, (e) => e.code === "worker-unavailable");
    f.dedicated[0].dispatchEvent(new Event("error"));
    await rejected;
    assert.equal(f.dedicated.length, 1);
  } finally {
    await f.close();
  }
});
for (const response of [
  { event: "state", state: { locked: true } },
  { id: 99999, result: null },
])
  test(
    "any response prevents startup failover: " + JSON.stringify(response),
    async () => {
      const f = startupFixture();
      try {
        const pending = f.vault.list(),
          rejected = assert.rejects(
            pending,
            (e) => e.code === "worker-unavailable",
          );
        f.shared[0].port.emit(response);
        f.shared[0].dispatchEvent(new Event("error"));
        await rejected;
        assert.equal(f.dedicated.length, 0);
      } finally {
        await f.close();
      }
    },
  );
test("authentication denial never retries in another worker", async () => {
  const f = startupFixture();
  try {
    const pending = f.vault.list(),
      rejected = assert.rejects(pending, (e) => e.code === "locked"),
      request = f.shared[0].port.sent.at(-1);
    f.shared[0].port.emit({
      id: request.id,
      error: "locked",
      state: { locked: true },
    });
    await rejected;
    f.shared[0].dispatchEvent(new Event("error"));
    assert.equal(f.dedicated.length, 0);
    assert.equal(f.vault.locked, true);
  } finally {
    await f.close();
  }
});
test("dispatching a private operation permanently prevents startup failover and argument replay", async () => {
  const f = startupFixture();
  try {
    const pending = f.vault.unlock(
        "A".repeat(40),
        "PRIVATE_STARTUP_PASS_CANARY",
      ),
      rejected = assert.rejects(
        pending,
        (e) => e.code === "worker-unavailable",
      );
    f.shared[0].dispatchEvent(new Event("error"));
    await rejected;
    assert.equal(f.dedicated.length, 0);
  } finally {
    await f.close();
  }
});
test("messageerror and explicit lock never trigger startup failover", async () => {
  for (const action of ["messageerror", "lock"]) {
    const f = startupFixture();
    try {
      const pending = f.vault.list(),
        rejected = assert.rejects(pending);
      if (action === "lock") f.vault.lock();
      else f.shared[0].port.dispatchEvent(new Event("messageerror"));
      f.shared[0].dispatchEvent(new Event("error"));
      await rejected;
      assert.equal(f.dedicated.length, 0);
    } finally {
      await f.close();
    }
  }
});
test("replacement constructor denial stays unavailable instead of using a page-local vault", async () => {
  const f = startupFixture({ denyDedicated: true });
  try {
    const pending = f.vault.list(),
      rejected = assert.rejects(
        pending,
        (e) => e.code === "worker-unavailable",
      );
    f.shared[0].dispatchEvent(new Event("error"));
    await rejected;
    assert.equal(f.vault.sessionMode, "shared");
    await assert.rejects(
      f.vault.list(),
      (e) => e.code === "worker-unavailable",
    );
  } finally {
    await f.close();
  }
});
test("failed keeper cleanup and stale pagehide do not disconnect a later keeper", async () => {
  const f = startupFixture({ keeper: true });
  let other;
  try {
    const pending = f.vault.list();
    f.shared[1].dispatchEvent(new Event("error"));
    assert.equal(f.shared[0].port.closed, true);
    const request = f.dedicated[0].sent.at(-1);
    f.dedicated[0].emit({
      id: request.id,
      result: { keys: [] },
      state: { locked: true },
    });
    await pending;
    other = createSessionVault("SYNTHETIC_STARTUP_USER_123");
    const newKeeper = f.shared[2];
    f.pagehide[0]();
    assert.equal(newKeeper.port.closed, false);
    await other.close();
    f.pagehide[1]();
    assert.equal(newKeeper.port.closed, true);
  } finally {
    await other?.close();
    await f.close();
  }
});
test("list deadline is retained across startup retry and a settled old timer cannot lock the new transport", async () => {
  const saved = {
      setTimeout: globalThis.setTimeout,
      clearTimeout: globalThis.clearTimeout,
    },
    timers = [];
  globalThis.setTimeout = (fn, ms) => {
    timers.push({ fn, ms });
    return timers.length;
  };
  globalThis.clearTimeout = () => {};
  const f = startupFixture();
  try {
    const pending = f.vault.list();
    assert.equal(timers[0].ms, 8000);
    f.shared[0].dispatchEvent(new Event("error"));
    const request = f.dedicated[0].sent.at(-1);
    f.dedicated[0].emit({
      id: request.id,
      result: { keys: [] },
      state: { locked: true },
    });
    await pending;
    const count = f.dedicated[0].sent.length;
    timers[0].fn();
    assert.equal(f.dedicated[0].sent.length, count);
    const late = f.vault.list(),
      rejected = assert.rejects(late, (e) => e.code === "locked");
    timers[1].fn();
    await rejected;
    f.dedicated[0].dispatchEvent(new Event("error"));
    assert.equal(f.dedicated.length, 1);
  } finally {
    await f.close();
    globalThis.setTimeout = saved.setTimeout;
    globalThis.clearTimeout = saved.clearTimeout;
  }
});

test("failed transport never restores unlocked UI state from late messages", async () => {
  const f = startupFixture();
  try {
    const pending = f.vault.list(),
      request = f.shared[0].port.sent.at(-1);
    f.shared[0].port.emit({
      id: request.id,
      result: { keys: [] },
      state: { locked: true },
    });
    await pending;
    f.shared[0].dispatchEvent(new Event("error"));
    f.shared[0].port.emit({
      event: "state",
      state: {
        locked: false,
        fingerprint: "A".repeat(40),
        expiresAt: Date.now() + 60000,
      },
    });
    assert.equal(f.vault.locked, true);
    assert.equal(f.vault.fingerprint, null);
    assert.equal(f.dedicated.length, 0);
  } finally {
    await f.close();
  }
});
test("old shared client fallback cannot retire a newer keeper generation", async () => {
  const f = startupFixture({ keeper: true });
  let second, third;
  try {
    second = createSessionVault("SYNTHETIC_STARTUP_USER_123");
    const oldKeeper = f.shared[0],
      a = f.shared[1],
      b = f.shared[2];
    a.dispatchEvent(new Event("error"));
    assert.equal(oldKeeper.port.closed, true);
    third = createSessionVault("SYNTHETIC_STARTUP_USER_123");
    const newKeeper = f.shared[3];
    b.dispatchEvent(new Event("error"));
    assert.equal(second.sessionMode, "dedicated");
    assert.equal(newKeeper.port.closed, false);
    f.pagehide[0]();
    assert.equal(newKeeper.port.closed, false);
  } finally {
    await second?.close();
    await third?.close();
    await f.close();
  }
});
