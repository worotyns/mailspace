// SPDX-License-Identifier: AGPL-3.0-only
// Shared/dedicated worker kernel. Dependencies are injected for boundary tests.
const MAX_IDLE = 300_000;
const safeErrors = new Set([
  "locked",
  "bad-passphrase",
  "missing-key",
  "invalid-fingerprint",
  "untrusted-recipient",
  "key-changed",
  "sender-key-mismatch",
  "unusable-key",
  "message-limit",
  "invalid-signature",
  "decryption-failed",
  "invalid-email",
  "invalid-recipients",
  "unprotected-key",
  "invalid-key",
  "operation-failed",
]);
const bytes = (value, limit) =>
  (typeof value === "string" &&
    new TextEncoder().encode(value).length <= limit) ||
  (value instanceof Uint8Array && value.length <= limit);
const address = (value) =>
  typeof value === "string" && value.length <= 254 && !/[\r\n\0]/.test(value);
function validArgs(op, args) {
  if (!Array.isArray(args)) return false;
  switch (op) {
    case "list":
    case "touch":
      return args.length === 0;
    case "unlock":
      return (
        args.length === 2 &&
        typeof args[0] === "string" &&
        /^(?:[A-Fa-f0-9]{40}|[A-Fa-f0-9]{64})$/.test(args[0]) &&
        typeof args[1] === "string" &&
        new TextEncoder().encode(args[1]).length <= 1024
      );
    case "encryptSigned":
      return (
        args.length === 3 &&
        args[0] instanceof Uint8Array &&
        args[0].length <= 4 << 20 &&
        address(args[1]) &&
        Array.isArray(args[2]) &&
        args[2].length <= 100 &&
        args[2].every(address)
      );
    case "signDetached":
      return (
        args.length === 2 &&
        args[0] instanceof Uint8Array &&
        args[0].length <= 4 << 20 &&
        address(args[1])
      );
    case "decryptSigned":
      return args.length === 2 && bytes(args[0], 6 << 20) && address(args[1]);
    case "verifyDetached":
      return (
        args.length === 3 &&
        args[0] instanceof Uint8Array &&
        args[0].length <= 4 << 20 &&
        bytes(args[1], 256 << 10) &&
        address(args[2])
      );
    default:
      return false;
  }
}
export function createSessionKernel({
  authenticate,
  createVault,
  now = Date.now,
}) {
  const connections = new Set();
  let active = null,
    queue = Promise.resolve(),
    waiting = 0;
  function state(entry = active) {
    return {
      locked: !entry || entry.vault.locked,
      fingerprint: entry?.vault.fingerprint || null,
      expiresAt: entry ? Math.min(entry.expiresAt, entry.usedAt + MAX_IDLE) : 0,
    };
  }
  function broadcast(entry = active) {
    for (const connection of connections) {
      if (
        entry &&
        connection.binding === entry.binding &&
        connection.userID === entry.userID
      ) {
        try {
          connection.emit({ event: "state", state: state(entry) });
        } catch {}
      }
    }
  }
  function lock(entry = active) {
    if (!entry) return;
    entry.epoch++;
    entry.vault.lock();
    broadcast(entry);
  }
  function expire() {
    if (
      active &&
      (now() >= active.expiresAt ||
        (!active.vault.locked && now() - active.usedAt >= MAX_IDLE))
    )
      lock(active);
  }
  async function authorization(connection, expected) {
    let session;
    try {
      session = await authenticate();
    } catch {
      lock();
      throw Object.assign(new Error(), { code: "locked" });
    }
    if (
      !session ||
      session.userID !== connection.userID ||
      !/^[a-f0-9]{64}$/.test(session.binding || "") ||
      !Number.isFinite(session.expiresAt) ||
      session.expiresAt <= now() ||
      (connection.binding && connection.binding !== session.binding) ||
      (expected && expected.binding !== session.binding)
    ) {
      lock();
      throw Object.assign(new Error(), { code: "locked" });
    }
    connection.binding ||= session.binding;
    return session;
  }
  function connect(emit) {
    const connection = {
      emit,
      userID: null,
      binding: null,
      epoch: 0,
      closed: false,
    };
    connections.add(connection);
    return {
      async request(message) {
        if (
          !message ||
          !Number.isSafeInteger(message.id) ||
          typeof message.userID !== "string" ||
          !/^[A-Za-z0-9_-]{16,128}$/.test(message.userID)
        )
          return { id: message?.id, error: "invalid-request" };
        if (connection.closed) return { id: message.id, error: "locked" };
        if (connection.userID && connection.userID !== message.userID)
          return { id: message.id, error: "locked" };
        connection.userID ||= message.userID;
        if (message.op === "lock") {
          connection.epoch++;
          // Lock grants no access and returns no active-session metadata. An
          // unbound page must still revoke a key retained by a previous view.
          lock(active);
          return {
            id: message.id,
            result: null,
            state: { locked: true, fingerprint: null, expiresAt: 0 },
          };
        }
        if (message.op === "disconnect") {
          this.close();
          return null;
        }
        if (!validArgs(message.op, message.args) || waiting >= 16)
          return { id: message.id, error: "invalid-request" };
        const connectionEpoch = connection.epoch;
        waiting++;
        const execute = async () => {
          let result,
            operationEntry = null,
            rechecked = false;
          try {
            if (connection.closed || connectionEpoch !== connection.epoch)
              throw Object.assign(new Error(), { code: "locked" });
            const session = await authorization(connection);
            if (connection.closed || connectionEpoch !== connection.epoch)
              throw Object.assign(new Error(), { code: "locked" });
            if (
              active &&
              (active.binding !== session.binding ||
                active.userID !== session.userID)
            ) {
              lock(active);
              const previous = active;
              active = null;
              await previous.vault.close();
            }
            if (!active)
              active = {
                vault: createVault(session.userID),
                userID: session.userID,
                binding: session.binding,
                expiresAt: session.expiresAt,
                usedAt: now(),
                epoch: 0,
              };
            const entry = active;
            operationEntry = entry;
            entry.expiresAt = Math.min(entry.expiresAt, session.expiresAt);
            expire();
            const epoch = entry.epoch;
            result =
              message.op === "touch"
                ? null
                : await entry.vault[message.op](...message.args);
            rechecked = true;
            await authorization(connection, entry);
            if (
              connection.closed ||
              entry !== active ||
              epoch !== entry.epoch ||
              connectionEpoch !== connection.epoch
            )
              throw Object.assign(new Error(), { code: "locked" });
            if (message.op !== "list") entry.usedAt = now();
            expire();
            // Metadata listing is allowed while locked; crypto output is not.
            if (
              !["list", "verifyDetached"].includes(message.op) &&
              entry.vault.locked
            )
              throw Object.assign(new Error(), { code: "locked" });
            broadcast(entry);
            return { id: message.id, result, state: state(entry) };
          } catch (error) {
            result = null;
            if (operationEntry && !rechecked) {
              try {
                await authorization(connection, operationEntry);
              } catch {
                error = { code: "locked" };
              }
            }
            if (
              active &&
              active.binding === connection.binding &&
              active.userID === connection.userID
            )
              broadcast(active);
            return {
              id: message.id,
              error: safeErrors.has(error?.code)
                ? error.code
                : "operation-failed",
              state:
                !connection.closed &&
                active &&
                active.binding === connection.binding &&
                active.userID === connection.userID
                  ? state(active)
                  : { locked: true, fingerprint: null, expiresAt: 0 },
            };
          } finally {
            waiting--;
          }
        };
        const pending = queue.then(execute, execute);
        queue = pending.then(
          () => {},
          () => {},
        );
        return pending;
      },
      close() {
        connection.closed = true;
        connection.epoch++;
        connections.delete(connection);
      },
    };
  }
  return {
    connect,
    expire,
    lock,
    dispose() {
      lock();
      for (const c of connections) c.closed = true;
      connections.clear();
      return active?.vault.close();
    },
  };
}
