// SPDX-License-Identifier: AGPL-3.0-only
// Frames below contain trusted app pages only, never arbitrary URLs/mail HTML.
const allowed =
  /^\/(settings|profile|workspaces|accounts|keys|rules|labels|automation|secure\/compose|messages\/read|outbox|archive\/preview)(\/|$)/;
const recordedStates = new Set([
  "pending",
  "sending",
  "accepted",
  "rejected",
  "unknown",
]);
let dialog,
  confirmation,
  content,
  title,
  frame,
  loading,
  pendingURL,
  pendingAction,
  dirty = false,
  ending = false;
function preventUnload(event) {
  if (!dirty) return;
  event.preventDefault();
  event.returnValue = "";
}
function setDirty(value) {
  dirty = value;
  dialog?.setAttribute("closedby", dirty ? "none" : "closerequest");
  if (dirty) window.addEventListener("beforeunload", preventUnload);
  else window.removeEventListener("beforeunload", preventUnload);
}
function clearFrame() {
  pendingURL = null;
  loading?.remove();
  loading = null;
  content?.removeAttribute("aria-busy");
  const previous = frame;
  frame = null;
  if (!previous) return;
  try {
    // This action has already passed the discard guard. Clear this document's
    // private UI without relocking a shared worker used by another app page.
    previous.contentWindow?.dispatchEvent(
      new Event("mailspace:discard-composer"),
    );
    previous.contentWindow?.dispatchEvent(new Event("mailspace:clear-private"));
  } catch {}
  previous.src = "about:blank";
  previous.remove();
}
function guard(action) {
  if (!dirty) {
    action();
    return;
  }
  pendingAction = action;
  if (!confirmation.open) confirmation.showModal();
}
function requestClose() {
  guard(() => dialog.close());
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
  content.className = "app-panel-content relative";
  dialog.append(header, content);
  confirmation = document.createElement("dialog");
  confirmation.id = "app-panel-discard";
  // These existing dialog utilities are also present in server templates.
  confirmation.className =
    "m-auto w-[94vw] max-w-md rounded-2xl border border-line bg-panel p-5 text-ink backdrop:bg-black/40";
  confirmation.setAttribute("aria-labelledby", "app-discard-title");
  confirmation.setAttribute("aria-describedby", "app-discard-message");
  const heading = document.createElement("h2");
  heading.id = "app-discard-title";
  heading.className = "font-semibold";
  heading.textContent = "Discard this unsent message?";
  const message = document.createElement("p");
  message.id = "app-discard-message";
  message.className = "mt-3 text-sm text-muted";
  message.textContent =
    "There are unsent changes to this message or its attachments. Keep editing, or discard them before leaving.";
  const actions = document.createElement("div");
  actions.className = "mt-4 flex justify-end gap-3";
  const keep = document.createElement("button");
  keep.textContent = "Keep editing";
  keep.type = "button";
  keep.autofocus = true;
  keep.className = "rounded-lg border border-line px-3 py-2 text-sm";
  keep.addEventListener("click", () => confirmation.close());
  const discard = document.createElement("button");
  discard.textContent = "Discard and continue";
  discard.type = "button";
  discard.className = "rounded-lg bg-action px-3 py-2 text-sm text-white";
  discard.addEventListener("click", () => {
    const action = pendingAction;
    pendingAction = null;
    setDirty(false);
    confirmation.close();
    action?.();
  });
  actions.append(keep, discard);
  confirmation.append(heading, message, actions);
  confirmation.addEventListener("close", () => {
    pendingAction = null;
    confirmation.returnValue = "";
  });
  document.body.append(dialog, confirmation);
  dialog.addEventListener("cancel", (event) => {
    if (dirty && event.cancelable) {
      event.preventDefault();
      requestClose();
    }
  });
  dialog.addEventListener("close", () => {
    if (confirmation.open) confirmation.close();
    pendingAction = null;
    clearFrame();
    content.replaceChildren();
    setDirty(false);
    ending = false;
    dialog.returnValue = "";
    window.dispatchEvent(new Event("mailspace:panel-closed"));
  });
}
function replacePanel(url) {
  if (pendingURL === url.href) return;
  clearFrame();
  setDirty(false);
  ending = false;
  pendingURL = url.href;
  content.setAttribute("aria-busy", "true");
  loading = document.createElement("p");
  loading.setAttribute("role", "status");
  loading.className =
    "absolute inset-0 flex items-center justify-center p-6 text-sm text-muted";
  loading.textContent = "Loading…";
  content.append(loading);
  const openedFrame = document.createElement("iframe");
  frame = openedFrame;
  openedFrame.title = "Mailspace application panel";
  openedFrame.className = "app-panel-frame invisible";
  openedFrame.src = url.href;
  openedFrame.referrerPolicy = "same-origin";
  openedFrame.addEventListener("load", () => {
    if (frame !== openedFrame) return;
    pendingURL = null;
    loading?.remove();
    loading = null;
    content.removeAttribute("aria-busy");
    openedFrame.classList.remove("invisible");
    try {
      const current = new URL(openedFrame.contentWindow.location.href);
      if (ending || current.pathname === "/login") {
        setDirty(false);
        clearFrame();
        location.assign("/login");
        return;
      }
      if (current.origin === location.origin && current.pathname === "/") {
        if (current.searchParams.has("notice")) {
          const notice =
            openedFrame.contentDocument.querySelector(
              "[data-toast]",
            )?.textContent;
          setDirty(false);
          dialog.close();
          if (notice)
            window.dispatchEvent(
              new CustomEvent("mailspace:toast", {
                detail: { message: notice },
              }),
            );
        } else {
          guard(() => {
            clearFrame();
            location.assign(current.href);
          });
        }
        return;
      }
      title.textContent = openedFrame.contentDocument.title.replace(
        / · Mailspace$/,
        "",
      );
      // Do not erase a dirty notification emitted before the frame's load event.
    } catch {
      title.textContent =
        "Open this page directly if your session has expired.";
    }
  });
  content.append(openedFrame);
  if (!dialog.open) dialog.showModal();
}
function open(url) {
  setup();
  document.getElementById("command-menu")?.close();
  guard(() => replacePanel(url));
}
function ownURL(value) {
  if (typeof value !== "string") return null;
  try {
    const url = new URL(value, location.href);
    return url.origin === location.origin ? url : null;
  } catch {
    return null;
  }
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
      link.hasAttribute("data-inline-decrypt") ||
      (link.target && link.target !== "_self") ||
      link.dataset.openDialog ||
      link.dataset.threadOpen
    )
      return;
    const url = ownURL(link.href);
    if (!url || !allowed.test(url.pathname)) return;
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
    const url = ownURL(data.location);
    if (!url || url.pathname !== "/outbox" || !recordedStates.has(data.state))
      return;
    setDirty(false);
    if (confirmation.open) confirmation.close();
    pendingAction = null;
    dialog.close();
    window.dispatchEvent(
      new CustomEvent("mailspace:sent", {
        detail: { state: data.state, location: url.pathname + url.search },
      }),
    );
    return;
  }
  if (data?.type === "mailspace:panel-dirty" && typeof data.dirty === "boolean")
    setDirty(data.dirty);
  if (data?.type === "mailspace:panel-navigate") {
    const url = ownURL(data.href);
    if (!url) return;
    if (url.pathname === "/") {
      guard(() => {
        clearFrame();
        location.assign(url.href);
      });
    } else if (allowed.test(url.pathname) || url.pathname === "/compose") {
      guard(() => replacePanel(url));
    }
  }
  if (data?.type === "mailspace:session-ending") {
    ending = true;
    setDirty(false);
    try {
      frame.contentWindow.dispatchEvent(
        new Event("mailspace:discard-composer"),
      );
    } catch {}
    window.dispatchEvent(new Event("mailspace:lock"));
  }
});
