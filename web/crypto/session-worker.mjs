// SPDX-License-Identifier: AGPL-3.0-only
import { LocalVault } from "./vault.mjs";
import { VaultStore } from "./store.mjs";
import { createSessionKernel } from "./session-core.mjs";

// No RPC argument enters this request. This is the worker's only network call.
const kernel = createSessionKernel({
  async authenticate() {
    const response = await fetch("/api/crypto/session", {
      method: "GET",
      credentials: "same-origin",
      cache: "no-store",
      redirect: "error",
      signal: AbortSignal.timeout(5000),
    });
    if (!response.ok) throw new Error("session unavailable");
    const value = await response.json();
    return {
      userID: value.userID,
      binding: value.binding,
      expiresAt: value.expiresAt,
    };
  },
  createVault(userID) {
    return new LocalVault(new VaultStore(userID));
  },
});
function attach(port) {
  const connection = kernel.connect((value) => port.postMessage(value));
  port.addEventListener("message", async (event) => {
    const response = await connection.request(event.data);
    if (response) {
      try {
        port.postMessage(response);
      } catch {
        connection.close();
      }
    }
    if (event.data?.op === "disconnect") {
      connection.close();
      port.close?.();
    }
  });
  port.addEventListener("messageerror", () => connection.close());
  port.start?.();
}
if ("onconnect" in self)
  self.addEventListener("connect", (event) => attach(event.ports[0]));
else attach(self);
setInterval(() => kernel.expire(), 1000);
