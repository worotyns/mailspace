// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { createSessionKernel } from "../crypto/session-core.mjs";
const user = "SYNTHETIC_WORKER_USER_123",
  fingerprint = "A".repeat(40);
function setup() {
  let clock = 1000,
    session = { userID: user, binding: "a".repeat(64), expiresAt: 900_000 },
    created = 0,
    authenticates = 0;
  const vaults = [];
  class FakeVault {
    locked = true;
    fingerprint = null;
    epoch = 0;
    pending = null;
    lock() {
      this.epoch++;
      this.locked = true;
      this.fingerprint = null;
    }
    async close() {
      this.lock();
    }
    async list() {
      return { locked: this.locked, fingerprint: this.fingerprint, keys: [] };
    }
    async unlock() {
      this.locked = false;
      this.fingerprint = fingerprint;
      return fingerprint;
    }
    async verifyDetached(data) {
      return { data, signatures: [] };
    }
    async encryptSigned() {
      throw { code: "untrusted-recipient" };
    }
    async decryptSigned() {
      const epoch = this.epoch;
      if (this.locked) throw { code: "locked" };
      if (this.pending) await this.pending;
      if (epoch !== this.epoch) throw { code: "locked" };
      return {
        data: new TextEncoder().encode("PRIVATE_DECRYPTED_CANARY"),
        provenance: "client-decrypted",
      };
    }
  }
  const kernel = createSessionKernel({
    authenticate: async (...args) => {
      assert.equal(args.length, 0, "RPC args reached authentication");
      authenticates++;
      return session;
    },
    createVault() {
      created++;
      const vault = new FakeVault();
      vaults.push(vault);
      return vault;
    },
    now: () => clock,
  });
  let id = 0;
  const connect = () => {
    const events = [];
    const port = kernel.connect((event) => events.push(event));
    return {
      port,
      events,
      call: (op, args = [], owner = user) =>
        port.request({ id: ++id, userID: owner, op, args }),
    };
  };
  return {
    kernel,
    connect,
    vaults,
    setSession(value) {
      session = value;
    },
    session: () => session,
    setClock(value) {
      clock = value;
    },
    created: () => created,
    authenticates: () => authenticates,
  };
}
test("two local ports reuse one unlocked vault and disconnect does not lock another reader", async () => {
  const f = setup(),
    a = f.connect(),
    b = f.connect();
  await a.call("list");
  await b.call("list");
  const unlocked = await a.call("unlock", [
    fingerprint,
    "PRIVATE_PASSPHRASE_CANARY",
  ]);
  assert.equal(unlocked.state.locked, false);
  const result = await b.call("decryptSigned", [
    new Uint8Array([1]),
    "sender@example.test",
  ]);
  assert.equal(result.result.provenance, "client-decrypted");
  assert.equal(f.created(), 1);
  assert.equal(f.authenticates(), 8);
  a.port.close();
  assert.equal((await b.call("list")).state.locked, false);
  assert.equal(
    b.events.some((event) => event.state.locked === false),
    true,
  );
  await f.kernel.dispose();
});
test("same-user new login and another owner cannot reuse a previously unlocked session", async () => {
  const f = setup(),
    a = f.connect();
  await a.call("unlock", [fingerprint, "secret"]);
  f.setSession({ ...f.session(), binding: "b".repeat(64) });
  const stale = await a.call("decryptSigned", [
    new Uint8Array([1]),
    "sender@example.test",
  ]);
  assert.equal(stale.error, "locked");
  assert.equal(stale.result, undefined);
  const b = f.connect();
  assert.equal((await b.call("list")).state.locked, true);
  await b.call("unlock", [fingerprint, "second-secret"]);
  f.setSession({
    ...f.session(),
    userID: "ANOTHER_WORKER_USER_456",
    binding: "c".repeat(64),
  });
  assert.equal(
    (
      await b.call("decryptSigned", [
        new Uint8Array([1]),
        "sender@example.test",
      ])
    ).error,
    "locked",
  );
  await f.kernel.dispose();
});
test("explicit lock cancels pending decryption before plaintext can return", async () => {
  const f = setup(),
    a = f.connect(),
    b = f.connect();
  await a.call("unlock", [fingerprint, "secret"]);
  await b.call("list");
  let release;
  f.vaults[0].pending = new Promise((resolve) => (release = resolve));
  const decrypt = a.call("decryptSigned", [
    new Uint8Array([1]),
    "sender@example.test",
  ]);
  await new Promise((resolve) => setImmediate(resolve));
  await b.call("lock");
  release();
  const result = await decrypt;
  assert.equal(result.error, "locked");
  assert.equal(result.result, undefined);
  assert.equal(f.vaults[0].locked, true);
  await f.kernel.dispose();
});
test("idle and hard session expiry lock memory, with no unknown RPC invocation", async () => {
  const f = setup(),
    a = f.connect();
  await a.call("unlock", [fingerprint, "secret"]);
  f.setClock(301001);
  f.kernel.expire();
  assert.equal(f.vaults[0].locked, true);
  await a.call("unlock", [fingerprint, "secret"]);
  assert.equal(f.vaults[0].locked, false);
  f.setClock(900001);
  f.kernel.expire();
  assert.equal(
    (
      await a.call("decryptSigned", [
        new Uint8Array([1]),
        "sender@example.test",
      ])
    ).error,
    "locked",
  );
  for (const op of [
    "exportPrivate",
    "generate",
    "trust",
    "constructor",
    "fetch",
  ]) {
    assert.equal((await a.call(op, [])).error, "invalid-request");
  }
  await f.kernel.dispose();
});
test("reauthentication after decryption blocks output when session changes mid-operation", async () => {
  const f = setup(),
    a = f.connect();
  await a.call("unlock", [fingerprint, "secret"]);
  let release;
  f.vaults[0].pending = new Promise((resolve) => (release = resolve));
  const decrypt = a.call("decryptSigned", [
    new Uint8Array([1]),
    "sender@example.test",
  ]);
  await new Promise((resolve) => setImmediate(resolve));
  f.setSession({ ...f.session(), binding: "d".repeat(64) });
  release();
  const response = await decrypt;
  assert.equal(response.error, "locked");
  assert.equal(response.result, undefined);
  await f.kernel.dispose();
});

test("only real activity touch extends an already-unlocked idle deadline", async () => {
  const f = setup(),
    a = f.connect();
  await a.call("unlock", [fingerprint, "secret"]);
  f.setClock(290_000);
  assert.equal((await a.call("touch")).state.locked, false);
  f.setClock(400_000);
  f.kernel.expire();
  assert.equal(f.vaults[0].locked, false);
  f.setClock(600_001);
  f.kernel.expire();
  assert.equal(f.vaults[0].locked, true);
  assert.equal((await a.call("touch")).error, "locked");
  await f.kernel.dispose();
});

test("public detached verification works while the private key remains locked", async () => {
  const f = setup(),
    a = f.connect();
  const result = await a.call("verifyDetached", [
    new Uint8Array([1]),
    new Uint8Array([2]),
    "sender@example.test",
  ]);
  assert.equal(result.error, undefined);
  assert.equal(result.state.locked, true);
  assert.deepEqual([...result.result.data], [1]);
  await f.kernel.dispose();
});
test("recipient validation errors preserve truthful healthy unlocked state", async () => {
  const f = setup(),
    a = f.connect();
  await a.call("unlock", [fingerprint, "secret"]);
  const result = await a.call("encryptSigned", [
    new Uint8Array([1]),
    "sender@example.test",
    ["untrusted@example.test"],
  ]);
  assert.equal(result.error, "untrusted-recipient");
  assert.equal(result.state.locked, false);
  assert.equal(result.state.fingerprint, fingerprint);
  assert.equal(f.vaults[0].locked, false);
  assert.equal(result.result, undefined);
  await f.kernel.dispose();
});
test("an unbound logout port immediately revokes another view's pending decrypt", async () => {
  const f = setup(),
    a = f.connect();
  await a.call("unlock", [fingerprint, "secret"]);
  let release;
  f.vaults[0].pending = new Promise((resolve) => (release = resolve));
  const pending = a.call("decryptSigned", [
    new Uint8Array([1]),
    "sender@example.test",
  ]);
  await new Promise((resolve) => setImmediate(resolve));
  const unbound = f.connect();
  const locked = await unbound.call("lock");
  assert.equal(locked.state.locked, true);
  assert.equal(locked.state.fingerprint, null);
  release();
  const result = await pending;
  assert.equal(result.error, "locked");
  assert.equal(result.result, undefined);
  await f.kernel.dispose();
});

test("failed crypto operations still reauthenticate before returning owned state", async () => {
  const f = setup(),
    a = f.connect();
  await a.call("unlock", [fingerprint, "secret"]);
  f.vaults[0].encryptSigned = async () => {
    f.setSession({ ...f.session(), binding: "e".repeat(64) });
    throw { code: "untrusted-recipient" };
  };
  const result = await a.call("encryptSigned", [
    new Uint8Array([1]),
    "sender@example.test",
    ["recipient@example.test"],
  ]);
  assert.equal(result.error, "locked");
  assert.equal(result.state.locked, true);
  assert.equal(f.vaults[0].locked, true);
  await f.kernel.dispose();
});

test("bad unlock broadcasts the actual locked state to other already-unlocked ports", async () => {
  const f = setup(),
    a = f.connect(),
    b = f.connect();
  await a.call("unlock", [fingerprint, "secret"]);
  await b.call("list");
  f.vaults[0].unlock = async function () {
    this.lock();
    throw { code: "bad-passphrase" };
  };
  const before = a.events.length;
  const failed = await b.call("unlock", [fingerprint, "wrong"]);
  assert.equal(failed.error, "bad-passphrase");
  assert.equal(failed.state.locked, true);
  assert.equal(
    a.events.slice(before).some((event) => event.state.locked === true),
    true,
  );
  await f.kernel.dispose();
});
