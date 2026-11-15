// SPDX-License-Identifier: AGPL-3.0-only
import { enhanceReadableMessages } from "./message-text.mjs";
// Only the fixed server-rendered history control is replaced. Paging never
// serializes fields or rebuilds mounted message/composer/decrypted DOM.
let pending = null;
function source(event) {
  return event.detail?.elt?.closest?.("[data-history-load]");
}
function restore() {
  const state = pending;
  if (!state || !state.feed.isConnected || !state.anchor?.isConnected) return;
  state.feed.scrollTop += state.anchor.getBoundingClientRect().top - state.top;
}
function failed() {
  if (!pending) return;
  const state = pending;
  pending = null;
  state.control.removeAttribute("aria-busy");
  const control = state.feed.querySelector("[data-history-controls]");
  if (control) {
    control.dataset.historyFailed = "true";
    control.querySelector("[data-history-status]").textContent =
      "Could not load older cached messages. Retry, or open Latest messages if the view changed.";
  }
}
document.addEventListener("htmx:beforeRequest", (event) => {
  const link = source(event);
  if (!link) return;
  const feed = link.closest("#timeline-feed");
  if (!feed || pending) {
    event.preventDefault();
    return;
  }
  const top = feed.getBoundingClientRect().top;
  const anchor = [...feed.querySelectorAll("article[data-message-key]")].find(
    (node) => node.getBoundingClientRect().bottom >= top,
  );
  const control = link.closest("[data-history-controls]");
  pending = {
    feed,
    control,
    focused: document.activeElement === link,
    anchor,
    top: anchor?.getBoundingClientRect().top,
    xhr: event.detail.xhr,
    rows: new Map(
      [...feed.querySelectorAll("[data-history-row]")].map((node) => [
        node.dataset.historyKey,
        node,
      ]),
    ),
  };
  control.setAttribute("aria-busy", "true");
  delete control.dataset.historyFailed;
  control.querySelector("[data-history-status]").textContent =
    "Loading older cached messages…";
});
document.addEventListener("htmx:afterSwap", (event) => {
  if (event.detail.target?.id !== "timeline-history" || !pending) return;
  const state = pending;
  if (!state.feed.isConnected) {
    pending = null;
    return;
  }
  for (const row of state.feed.querySelectorAll("[data-history-row]")) {
    const retained = state.rows.get(row.dataset.historyKey);
    if (retained && retained !== row) row.remove();
  }
  const csrf = state.feed.closest("#timeline-shell")?.dataset.csrf;
  if (csrf)
    for (const input of state.feed.querySelectorAll('input[name="csrf"]'))
      input.value = csrf;
  enhanceReadableMessages(state.feed);
  restore();
  state.control.removeAttribute("aria-busy");
  if (state.focused) {
    const next = state.feed.querySelector("[data-history-controls] a");
    next?.focus({ preventScroll: true });
  }
  pending = null;
  window.dispatchEvent(new Event("mailspace:history-loaded"));
});
document.addEventListener("htmx:afterRequest", (event) => {
  if (source(event) && event.detail.successful === false) failed();
});
for (const type of ["htmx:sendError", "htmx:timeout", "htmx:swapError"])
  document.addEventListener(type, (event) => {
    if (source(event)) failed();
  });
function clear() {
  const state = pending;
  pending = null;
  if (state) {
    state.control.removeAttribute("aria-busy");
    const status = state.control.querySelector("[data-history-status]");
    if (status) status.textContent = "";
    state.xhr?.abort();
  }
}
document.addEventListener("htmx:beforeSwap", (event) => {
  if (event.detail.target?.id === "timeline-shell") clear();
});
window.addEventListener("pagehide", clear);
const positions = new WeakMap();
document.addEventListener(
  "scroll",
  (event) => {
    const feed = event.target;
    if (feed.id !== "timeline-feed") return;
    const previous = positions.get(feed);
    positions.set(feed, feed.scrollTop);
    if (
      previous === undefined ||
      feed.scrollTop >= previous ||
      feed.scrollTop > 120 ||
      pending
    )
      return;
    const control = feed.querySelector("[data-history-controls]");
    if (!control || control.dataset.historyFailed === "true") return;
    control.querySelector("[data-history-load]")?.click();
  },
  { capture: true, passive: true },
);
