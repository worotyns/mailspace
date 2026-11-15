// SPDX-License-Identifier: AGPL-3.0-only
// Keep the native details/forms fallback when the Popover API is unavailable.
function enhanceLabels() {
  if (
    !("popover" in HTMLElement.prototype) ||
    typeof HTMLElement.prototype.showPopover !== "function"
  )
    return;
  for (const details of document.querySelectorAll(
    "details[data-label-picker]",
  )) {
    const summary = details.querySelector("summary");
    const options = details.querySelector("[data-label-options]");
    if (!summary || !options || !details.isConnected) continue;
    const reopen = details.open;
    const wrapper = document.createElement("span");
    wrapper.id = details.id;
    wrapper.className = "min-w-0 max-w-full";
    const trigger = document.createElement("button");
    trigger.type = "button";
    trigger.className = summary.className + " label-picker-toggle";
    trigger.title = "Edit message labels";
    trigger.setAttribute("aria-label", "Edit message labels");
    options.id = details.id + "-options";
    options.className = "label-popover";
    options.setAttribute("popover", "auto");
    options.setAttribute("role", "dialog");
    trigger.setAttribute("popovertarget", options.id);
    // Move only trusted label controls; no HTML sink or message body access.
    trigger.append(...summary.childNodes);
    wrapper.append(trigger, options);
    details.replaceWith(wrapper);
    const position = () => {
      const anchor = trigger.getBoundingClientRect();
      const box = options.getBoundingClientRect();
      const gap = 8;
      options.style.left =
        Math.max(gap, Math.min(anchor.left, innerWidth - box.width - gap)) +
        "px";
      const below = anchor.bottom + gap;
      const top =
        below + box.height <= innerHeight - gap
          ? below
          : anchor.top - box.height - gap;
      options.style.top =
        Math.max(gap, Math.min(top, innerHeight - box.height - gap)) + "px";
    };
    options.addEventListener("beforetoggle", (event) => {
      if (event.newState !== "open") return;
      const anchor = trigger.getBoundingClientRect();
      // Reserve a bounded box before first paint, then measure the open popup.
      const width = Math.min(352, innerWidth - 16);
      const height = Math.min(320, innerHeight * 0.6);
      options.style.left =
        Math.max(8, Math.min(anchor.left, innerWidth - width - 8)) + "px";
      options.style.top =
        Math.max(8, Math.min(anchor.bottom + 8, innerHeight - height - 8)) +
        "px";
      requestAnimationFrame(() => {
        if (options.isConnected && options.matches(":popover-open")) position();
      });
    });
    options.addEventListener("toggle", (event) => {
      if (event.newState === "open") position();
      else if (
        trigger.isConnected &&
        (document.activeElement === document.body ||
          options.contains(document.activeElement))
      )
        trigger.focus({ preventScroll: true });
    });
    // A successful membership update replaces this metadata widget only. Keep
    // its chooser/focus available for another independent label toggle.
    if (reopen) {
      options.showPopover();
      position();
      options.querySelector("[autofocus]")?.focus({ preventScroll: true });
    }
  }
}
function dismissLabels(event) {
  if (
    !("popover" in HTMLElement.prototype) ||
    typeof HTMLElement.prototype.showPopover !== "function"
  )
    return;
  for (const popover of document.querySelectorAll(
    ".label-popover:popover-open",
  )) {
    if (event?.target instanceof Node && popover.contains(event.target))
      continue;
    popover.hidePopover();
  }
}
enhanceLabels();
document.addEventListener("htmx:afterSwap", enhanceLabels);
// Native focus restoration may itself scroll a feed while opening. Dismiss
// only for user scrolling outside the chooser, not those programmatic events.
document.addEventListener("wheel", dismissLabels, {
  capture: true,
  passive: true,
});
document.addEventListener("touchmove", dismissLabels, {
  capture: true,
  passive: true,
});
window.addEventListener("resize", dismissLabels);
window.addEventListener("mailspace:lock", dismissLabels);
window.addEventListener("pagehide", dismissLabels);
