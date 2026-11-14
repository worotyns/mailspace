// SPDX-License-Identifier: AGPL-3.0-only
// Busy state begins after control serialization, preserving submitter consent.
(() => {
  const busy = new WeakMap();
  function requestTarget(event) {
    const elt = event.detail?.elt;
    return elt?.closest("form") || elt;
  }
  function start(form) {
    if (!form || busy.has(form)) return;
    const controls = form.matches("button,input,a")
      ? [form]
      : [...form.querySelectorAll("button,input[type=submit]")];
    const buttons = controls.map((button) => [button, button.disabled]);
    const active = document.activeElement;
    const focusID = controls.includes(active) ? active.id : "";
    busy.set(form, { buttons, focusID });
    form.setAttribute("aria-busy", "true");
    for (const [button] of buttons) {
      if (button.tagName === "A") button.setAttribute("aria-disabled", "true");
      else button.disabled = true;
    }
  }
  function finish(form) {
    const state = busy.get(form);
    if (!state) return;
    const { buttons, focusID } = state;
    for (const [button, disabled] of buttons) {
      if (button.tagName === "A") button.removeAttribute("aria-disabled");
      else button.disabled = disabled;
    }
    form.removeAttribute("aria-busy");
    busy.delete(form);
    // Disabling an active control can drop focus to body before an HTMX swap.
    // Restore only the same scoped ID, without stealing focus from another UI.
    if (focusID && document.activeElement === document.body) {
      const replacement = document.getElementById(focusID);
      if (replacement && !replacement.disabled)
        replacement.focus({ preventScroll: true });
    }
  }
  document.addEventListener("htmx:beforeRequest", (event) =>
    start(requestTarget(event)),
  );
  document.addEventListener("htmx:afterRequest", (event) =>
    finish(requestTarget(event)),
  );
  document.addEventListener(
    "click",
    (event) => {
      if (event.target.closest('[aria-busy="true"]')) {
        event.preventDefault();
        event.stopImmediatePropagation();
      }
    },
    true,
  );
  document.addEventListener("submit", (event) => {
    const form = event.target;
    if (form.method === "dialog") return;
    requestAnimationFrame(() => {
      if (!event.defaultPrevented) start(form);
    });
  });
  window.addEventListener("pageshow", () => {
    for (const form of document.querySelectorAll('[aria-busy="true"]'))
      finish(form);
  });
})();
