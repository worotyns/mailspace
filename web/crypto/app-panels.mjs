// SPDX-License-Identifier: AGPL-3.0-only
// Frames below contain trusted app pages only, never arbitrary URLs/mail HTML.
const allowed =
  /^\/(settings|profile|workspaces|accounts|keys|rules|labels|automation|secure\/compose|messages\/read|outbox|archive\/preview)(\/|$)/;
let dialog,
  content,
  title,
  frame,
  dirty = false,
  ending = false;
function requestClose() {
  if (dirty) {
    dialog.querySelector("[data-unsaved]").hidden = false;
    return;
  }
  dialog.close();
}
function setup() {
  if (dialog) return;
  dialog = document.createElement("dialog");
  dialog.id = "app-panel-dialog";
  dialog.className = "app-panel-dialog";
  dialog.setAttribute("aria-labelledby", "app-panel-title");
  const header = document.createElement("header");
  header.className = "app-panel-header";
  title = document.createElement("h2");
  title.id = "app-panel-title";
  title.textContent = "Settings";
  const close = document.createElement("button");
  close.type = "button";
  close.textContent = "Close";
  close.addEventListener("click", requestClose);
  header.append(title, close);
  content = document.createElement("div");
  content.className = "app-panel-content";
  const warning = document.createElement("div");
  warning.dataset.unsaved = "";
  warning.hidden = true;
  warning.className = "p-3 text-sm";
  const message = document.createElement("p");
  message.textContent =
    "There is unsent text in this composer. Keep editing or discard it before closing.";
  const keep = document.createElement("button");
  keep.textContent = "Keep editing";
  keep.type = "button";
  keep.addEventListener("click", () => {
    warning.hidden = true;
    frame?.focus();
  });
  const discard = document.createElement("button");
  discard.textContent = "Discard unsent text and close";
  discard.type = "button";
  discard.addEventListener("click", () => {
    dirty = false;
    dialog.close();
  });
  warning.append(message, keep, discard);
  dialog.append(header, warning, content);
  document.body.append(dialog);
  dialog.addEventListener("cancel", (event) => {
    if (dirty && event.cancelable) {
      event.preventDefault();
      warning.hidden = false;
    }
  });
  dialog.addEventListener("close", () => {
    try {
      frame?.contentWindow?.dispatchEvent(new Event("mailspace:lock"));
    } catch {}
    if (frame) frame.src = "about:blank";
    content.replaceChildren();
    frame = null;
    dirty = false;
    ending = false;
    warning.hidden = true;
    window.dispatchEvent(new Event("mailspace:panel-closed"));
  });
}
function open(url) {
  setup();
  document.getElementById("command-menu")?.close();
  if (frame && dirty) {
    dialog.querySelector("[data-unsaved]").hidden = false;
    return;
  }
  dirty = false;
  content.replaceChildren();
  frame = document.createElement("iframe");
  frame.title = "Mailspace application panel";
  frame.className = "app-panel-frame";
  frame.src = url.href;
  frame.referrerPolicy = "same-origin";
  frame.addEventListener("load", () => {
    try {
      const current = new URL(frame.contentWindow.location.href);
      if (ending || current.pathname === "/login") {
        location.assign("/login");
        return;
      }
      if (current.origin === location.origin && current.pathname === "/") {
        if (current.searchParams.has("notice")) {
          const notice =
            frame.contentDocument.querySelector("[data-toast]")?.textContent;
          dirty = false;
          dialog.close();
          if (notice)
            window.dispatchEvent(
              new CustomEvent("mailspace:toast", {
                detail: { message: notice },
              }),
            );
        } else location.assign(current.href);
        return;
      }
      title.textContent = frame.contentDocument.title.replace(
        / · Mailspace$/,
        "",
      );
      dirty = false;
    } catch {
      title.textContent =
        "Open this page directly if your session has expired.";
    }
  });
  content.append(frame);
  if (!dialog.open) dialog.showModal();
}
document.addEventListener(
  "click",
  (event) => {
    const link = event.target.closest("a[href]");
    if (
      !link ||
      event.defaultPrevented ||
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey ||
      link.hasAttribute("download") ||
      link.dataset.openDialog ||
      link.dataset.threadOpen
    )
      return;
    const url = new URL(link.href, location.href);
    if (url.origin !== location.origin || !allowed.test(url.pathname)) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    open(url);
  },
  true,
);
window.addEventListener("message", (event) => {
  if (
    !frame ||
    event.origin !== location.origin ||
    event.source !== frame.contentWindow
  )
    return;
  const data = event.data;
  if (data?.type === "mailspace:commands") {
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "k", ctrlKey: true }),
    );
    return;
  }
  if (data?.type === "mailspace:panel-sent") {
    if (
      typeof data.location !== "string" ||
      !data.location.startsWith("/outbox?")
    )
      return;
    dirty = false;
    dialog.close();
    window.dispatchEvent(
      new CustomEvent("mailspace:sent", {
        detail: { state: data.state, location: data.location },
      }),
    );
  }
  if (data?.type === "mailspace:panel-dirty") dirty = data.dirty === true;
  if (data?.type === "mailspace:panel-navigate") {
    const url = new URL(data.href, location.href);
    if (url.origin === location.origin && url.pathname === "/")
      location.assign(url.href);
  }
  if (data?.type === "mailspace:session-ending") {
    ending = true;
    window.dispatchEvent(new Event("mailspace:lock"));
  }
});
