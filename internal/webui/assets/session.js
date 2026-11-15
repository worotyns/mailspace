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
  // A Security panel may rotate the full session while the timeline stays
  // mounted. Fetch fresh authentication metadata from the server; no tokens,
  // seeds or form contents cross the child/parent message boundary.
  let syncing = false,
    stale = false,
    revision = 0,
    request;
  const notice = (message) =>
    window.dispatchEvent(
      new CustomEvent("mailspace:toast", { detail: { message } }),
    );
  async function refreshSession() {
    const shell = document.getElementById("timeline-shell");
    if (!shell) return;
    const userID = shell.dataset.userId;
    const current = ++revision;
    syncing = true;
    request?.abort();
    request = new AbortController();
    try {
      const response = await fetch("/api/session", {
        credentials: "same-origin",
        cache: "no-store",
        redirect: "error",
        signal: request.signal,
      });
      if (!response.ok) throw new Error("session unavailable");
      const data = await response.json();
      if (current !== revision) return;
      const active = document.getElementById("timeline-shell");
      const expires = Date.parse(data.expiresAt);
      if (
        !active ||
        active.dataset.userId !== userID ||
        data.user?.id !== userID ||
        typeof data.csrfToken !== "string" ||
        !/^[A-Za-z0-9_-]{32,128}$/.test(data.csrfToken) ||
        !Number.isFinite(expires) ||
        expires <= Date.now()
      )
        throw new Error("session changed");
      for (const node of document.querySelectorAll("[data-csrf]"))
        node.dataset.csrf = data.csrfToken;
      for (const input of document.querySelectorAll('input[name="csrf"]'))
        input.value = data.csrfToken;
      for (const node of document.querySelectorAll("[data-session-expires]"))
        node.dataset.sessionExpires = String(expires);
      stale = false;
    } catch {
      if (current !== revision) return;
      stale = true;
      notice(
        "Session changed. Reload the page or sign in again before continuing.",
      );
    } finally {
      if (current === revision) syncing = false;
    }
  }
  window.addEventListener("mailspace:session-rotated", refreshSession);
  window.addEventListener("mailspace:panel-closed", () => {
    if (stale && !syncing) void refreshSession();
  });
  window.addEventListener("pagehide", () => {
    revision++;
    syncing = false;
    stale = !!document.getElementById("timeline-shell");
    request?.abort();
  });
  window.addEventListener("pageshow", (event) => {
    if (event.persisted) void refreshSession();
  });
  const protectMutation = (event) => {
    if (!syncing && !stale) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    notice(
      syncing
        ? "Updating your session. Try again in a moment."
        : "Reload the page or sign in again before continuing.",
    );
  };
  document.addEventListener("submit", protectMutation, true);
  document.addEventListener("htmx:configRequest", (event) => {
    if (event.detail?.verb?.toLowerCase() !== "get") protectMutation(event);
  });
  document.addEventListener("submit", (event) => {
    if (!(event.target instanceof HTMLFormElement)) return;
    const path = new URL(event.target.action, location.href).pathname;
    if (
      path === "/auth/login" ||
      path === "/auth/mfa" ||
      path === "/auth/mfa/cancel" ||
      path.startsWith("/security/authenticator/") ||
      path === "/auth/logout" ||
      path === "/auth/logout-all"
    ) {
      lock();
      channel?.postMessage({ type: "lock" });
    }
  });
})();
