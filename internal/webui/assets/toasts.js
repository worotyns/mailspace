// SPDX-License-Identifier: AGPL-3.0-only
(() => {
  const region = document.createElement("section");
  region.className = "toast-stack";
  region.setAttribute("role", "region");
  region.setAttribute("aria-label", "Notifications");
  const announcer = document.createElement("div");
  announcer.className = "sr-only";
  announcer.setAttribute("role", "status");
  document.body.append(region, announcer);
  const native = typeof region.showPopover === "function";
  if (native) region.setAttribute("popover", "manual");
  else region.hidden = true;
  const queue = [];
  let modal = false;
  function sync() {
    modal = !!document.querySelector("dialog[open]");
    if (modal) {
      if (native && region.matches(":popover-open")) region.hidePopover();
      else if (!native) region.hidden = true;
      return;
    }
    if (region.childElementCount) {
      if (native && !region.matches(":popover-open")) region.showPopover();
      else if (!native) region.hidden = false;
    }
    while (queue.length && !modal) show(queue.shift());
  }
  function show(data) {
    const message = String(data?.message || "").slice(0, 600);
    if (!message.trim()) return;
    if (document.querySelector("dialog[open]")) {
      if (queue.length >= 20) queue.shift();
      queue.push(data);
      return;
    }
    const existing = [...region.children].find(
      (el) => el.dataset.message === message,
    );
    if (existing) return;
    const previous = document.activeElement,
      row = document.createElement("div");
    row.className = "toast-item";
    if (data.kind === "error") row.classList.add("toast-error");
    row.dataset.message = message;
    const text = document.createElement("p");
    text.textContent = message;
    const close = document.createElement("button");
    close.type = "button";
    close.textContent = "×";
    close.setAttribute("aria-label", "Dismiss notification");
    close.addEventListener("click", () => {
      const focused = row.contains(document.activeElement);
      row.remove();
      if (focused)
        (
          region.querySelector("button") ||
          (previous?.isConnected ? previous : null)
        )?.focus();
      if (!region.childElementCount) {
        if (native && region.matches(":popover-open")) region.hidePopover();
        else if (!native) region.hidden = true;
      }
    });
    row.append(text);
    if (typeof data.href === "string" && data.href.startsWith("/outbox?")) {
      const details = document.createElement("a");
      details.href = data.href;
      details.textContent = "Outbox details";
      row.append(details);
    }
    row.append(close);
    region.append(row);
    while (region.childElementCount > 3) {
      const oldest = [...region.children].find(
        (el) => !el.contains(document.activeElement),
      );
      if (!oldest) break;
      oldest.remove();
    }
    if (typeof document.ariaNotify === "function") document.ariaNotify(message);
    else {
      announcer.textContent = "";
      queueMicrotask(() => (announcer.textContent = message));
    }
    sync();
  }
  function scan(root = document) {
    for (const el of root.querySelectorAll(
      "[data-toast]:not([data-toast-seen])",
    )) {
      el.dataset.toastSeen = "true";
      if (window.self !== window.top || el.closest("dialog")) {
        el.hidden = false;
        continue;
      }
      show({
        message: el.textContent,
        kind: el.getAttribute("role") === "alert" ? "error" : "info",
      });
      el.hidden = true;
    }
  }
  window.addEventListener("mailspace:toast", (event) => show(event.detail));
  document.addEventListener("htmx:afterSwap", () => scan());
  document.addEventListener("close", sync, true);
  document.addEventListener("toggle", sync, true);
  new MutationObserver(sync).observe(document.body, {
    subtree: true,
    attributes: true,
    attributeFilter: ["open"],
  });
  scan();
})();
