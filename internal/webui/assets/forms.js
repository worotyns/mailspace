// SPDX-License-Identifier: AGPL-3.0-only
// Busy state begins after control serialization, preserving submitter consent.
(() => {
  const busy = new WeakMap();
  function start(form) {
    if (!form || busy.has(form)) return;
    const buttons = [...form.querySelectorAll("button")].map((button) => [
      button,
      button.disabled,
    ]);
    busy.set(form, buttons);
    form.setAttribute("aria-busy", "true");
    for (const [button] of buttons) button.disabled = true;
  }
  function finish(form) {
    const buttons = busy.get(form);
    if (!buttons) return;
    for (const [button, disabled] of buttons) button.disabled = disabled;
    form.removeAttribute("aria-busy");
    busy.delete(form);
  }
  document.addEventListener("htmx:beforeRequest", (event) =>
    start(event.detail.elt?.closest("form")),
  );
  document.addEventListener("htmx:afterRequest", (event) =>
    finish(event.detail.elt?.closest("form")),
  );
  document.addEventListener("submit", (event) => {
    const form = event.target;
    if (form.method === "dialog") return;
    requestAnimationFrame(() => {
      if (!event.defaultPrevented) start(form);
    });
  });
  window.addEventListener("pageshow", () => {
    for (const form of document.querySelectorAll('form[aria-busy="true"]'))
      finish(form);
  });
})();
