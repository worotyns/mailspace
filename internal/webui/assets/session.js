// SPDX-License-Identifier: AGPL-3.0-only
// Broadcast a lock only, never credentials or key material.
(() => {
  const channel =
    typeof BroadcastChannel === "function"
      ? new BroadcastChannel("mailspace-vault-lock")
      : null;
  const lock = () => window.dispatchEvent(new Event("mailspace:lock"));
  channel?.addEventListener("message", (event) => {
    if (event.data?.type === "lock") lock();
  });
  document.addEventListener("submit", (event) => {
    if (!(event.target instanceof HTMLFormElement)) return;
    const path = new URL(event.target.action, location.href).pathname;
    if (
      path === "/auth/login" ||
      path === "/auth/logout" ||
      path === "/auth/logout-all"
    ) {
      lock();
      channel?.postMessage({ type: "lock" });
    }
  });
})();
