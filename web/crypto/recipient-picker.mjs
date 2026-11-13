// SPDX-License-Identifier: AGPL-3.0-only
// Suggestions transmit only the typed recipient fragment to this mail server.
// Never read a message body, key, passphrase or decrypted reader selection.
export function recipientFragment(value) {
  let quoted = false,
    escaped = false,
    angle = 0,
    start = 0;
  for (let i = 0; i < value.length; i++) {
    const char = value[i];
    if (escaped) {
      escaped = false;
      continue;
    }
    if (char === "\\" && quoted) {
      escaped = true;
      continue;
    }
    if (char === '"') {
      quoted = !quoted;
      continue;
    }
    if (!quoted) {
      if (char === "<") angle++;
      if (char === ">") angle = Math.max(0, angle - 1);
      if ((char === "," || char === ";") && angle === 0) start = i + 1;
    }
  }
  return { prefix: value.slice(0, start), query: value.slice(start).trim() };
}
const pending = new Map();
let epoch = 0;
function clear() {
  epoch++;
  for (const state of pending.values()) {
    clearTimeout(state.timer);
    state.request?.abort();
    state.list?.replaceChildren();
  }
  pending.clear();
}
document.addEventListener("input", (event) => {
  const input = event.target;
  if (!input.matches?.("[data-recipient-picker]")) return;
  const list = document.getElementById(input.getAttribute("list"));
  if (!list) return;
  const previous = pending.get(input);
  if (previous) {
    clearTimeout(previous.timer);
    previous.request?.abort();
  }
  const snapshot = input.value,
    { prefix, query } = recipientFragment(snapshot);
  list.replaceChildren();
  if (query.length < 2 || query.length > 254) return;
  const state = { list, request: new AbortController() };
  pending.set(input, state);
  const current = epoch;
  state.timer = setTimeout(async () => {
    try {
      const response = await fetch(
        "/people/suggestions?" + new URLSearchParams({ q: query }),
        {
          credentials: "same-origin",
          cache: "no-store",
          redirect: "error",
          signal: state.request.signal,
        },
      );
      if (!response.ok) return;
      const suggestions = await response.json();
      if (
        current !== epoch ||
        input.value !== snapshot ||
        !input.isConnected ||
        !Array.isArray(suggestions)
      )
        return;
      for (const item of suggestions.slice(0, 20)) {
        if (typeof item.email !== "string" || typeof item.name !== "string")
          continue;
        const option = document.createElement("option");
        option.value = prefix + (prefix ? " " : "") + item.email;
        option.label = item.name;
        list.append(option);
      }
    } catch {
      /* Free-form recipient entry stays available when suggestions fail. */
    }
  }, 250);
});
window.addEventListener("pagehide", clear);
window.addEventListener("mailspace:lock", clear);
document.addEventListener("htmx:beforeSwap", (event) => {
  if (["timeline-shell", "thread-content"].includes(event.detail.target?.id))
    clear();
});
