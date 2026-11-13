// SPDX-License-Identifier: AGPL-3.0-only
import "./recipient-picker.mjs";
import "./app-panels.mjs";
// Plain replies use HTMX. Encrypted mode has no serializable body field and
// intercepts submission before HTMX; the crypto module uploads ciphertext only.
let cleanup = () => {};
const attachmentViewers = new Set();
const attachmentRequests = new Set();
let attachmentEpoch = 0;
function clearAttachments() {
  attachmentEpoch++;
  for (const request of attachmentRequests) request.abort();
  attachmentRequests.clear();
  for (const viewer of attachmentViewers) {
    if (viewer.triggerButton?.isConnected) {
      viewer.triggerButton.disabled = false;
      viewer.triggerButton.setAttribute("aria-label", "Attachments");
      viewer.triggerButton.title = "Attachments";
    }
    viewer.dispose();
  }
  attachmentViewers.clear();
}
function initialize() {
  cleanup();
  cleanup = () => {};
  const shell = document.getElementById("timeline-shell");
  if (!shell) return;
  const scope = shell.querySelector("#thread-drawer")?.open
    ? shell.querySelector("#thread-content")
    : shell;
  const feed =
    scope.querySelector("#thread-feed") ||
    scope.querySelector("#timeline-feed");
  if (feed?.dataset.jumpLatest.trim() === "true")
    feed.scrollTop = feed.scrollHeight;
  const selected = scope.querySelector('[data-selected="true"]');
  if (feed && selected)
    feed.scrollTop +=
      selected.getBoundingClientRect().top -
      feed.getBoundingClientRect().top -
      feed.clientHeight / 3;
  const form = scope.querySelector("[data-reply-form]");
  if (!form) return;
  const bindingEvents = new AbortController();
  const options = { signal: bindingEvents.signal };
  const body = form.querySelector("textarea"),
    toggle = form.querySelector("[data-encrypt-toggle]"),
    send = form.querySelector("[data-send]"),
    status = form.querySelector("[data-reply-status]"),
    state = form.querySelector("[data-encryption-state]");
  const required = form.dataset.encryptionRequired === "true";
  let encrypted = required,
    controller,
    disposed = false,
    pending;
  function paint() {
    toggle.setAttribute("aria-pressed", String(encrypted));
    toggle.textContent = encrypted ? "🔒 Encrypt & sign on" : "Encrypt & sign";
    state.textContent = encrypted
      ? "Encrypted locally before sending"
      : "No encryption selected";
    send.textContent = encrypted
      ? "Send encrypted & signed"
      : "Send unencrypted";
    if (encrypted) {
      body.removeAttribute("name");
      form.setAttribute("hx-disable", "");
    } else {
      body.name = "body";
      form.removeAttribute("hx-disable");
    }
    send.disabled = false;
  }
  async function ready() {
    pending ||= import("./timeline-crypto.mjs").then(({ InlineEncryption }) => {
      if (disposed) return;
      controller = new InlineEncryption(shell, form);
      return controller;
    });
    const result = await pending;
    if (!result || disposed) throw new Error("composer closed");
    return result;
  }
  toggle.addEventListener(
    "click",
    async () => {
      if (encrypted && !required) {
        controller?.lock(false);
        encrypted = false;
        paint();
        return;
      }
      encrypted = true;
      paint();
      try {
        await (await ready()).open();
      } catch {
        status.textContent =
          "Local encryption is unavailable. No message was sent.";
      }
    },
    options,
  );
  form.addEventListener(
    "submit",
    async (event) => {
      if (!encrypted) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      try {
        await (await ready()).send();
      } catch {
        status.textContent =
          "Could not encrypt. Nothing was sent; check your local keys.";
      }
    },
    { capture: true, signal: bindingEvents.signal },
  );
  // Defense against programmatic HTMX triggers, including already-bound forms.
  form.addEventListener(
    "htmx:configRequest",
    (event) => {
      if (encrypted) event.preventDefault();
    },
    options,
  );
  cleanup = () => {
    disposed = true;
    bindingEvents.abort();
    controller?.dispose();
  };
  paint();
}
document.addEventListener("click", (event) => {
  const thread = event.target.closest("[data-thread-open]");
  if (
    thread &&
    typeof document.getElementById("thread-drawer")?.showModal === "function"
  ) {
    const drawer = document.getElementById("thread-drawer");
    if (!drawer.open) drawer.showModal();
  }
  const openPeople = event.target.closest("[data-open-people]");
  if (openPeople) {
    document.getElementById("command-menu")?.close();
    document.getElementById("people-directory")?.showModal();
  }
  const opener = event.target.closest("[data-open-dialog]");
  if (opener) {
    const dialog = document.getElementById(opener.dataset.openDialog);
    if (dialog?.showModal) {
      event.preventDefault();
      if (dialog.id === "command-menu") prepareContextCommands();
      dialog.showModal();
    }
  }
  const close = event.target.closest("[data-close-dialog]");
  if (close) close.closest("dialog")?.close();
});
document.addEventListener("htmx:beforeSwap", (event) => {
  if (["timeline-shell", "thread-content"].includes(event.detail.target?.id)) {
    cleanup();
    clearAttachments();
  } else if (event.detail.target?.id?.includes("membership-")) {
    clearAttachments();
  }
  if (
    event.detail.xhr?.status === 422 &&
    event.detail.target?.hasAttribute("data-reply-status")
  ) {
    event.detail.shouldSwap = true;
    event.detail.isError = false;
  }
});
document.addEventListener("htmx:afterSwap", (event) => {
  if (["timeline-shell", "thread-content"].includes(event.detail.target?.id))
    initialize();
});
window.addEventListener("pagehide", () => cleanup());
initialize();

document.addEventListener("htmx:beforeRequest", () => {
  document.getElementById("request-error")?.setAttribute("hidden", "");
});
for (const event of ["htmx:responseError", "htmx:sendError", "htmx:timeout"])
  document.addEventListener(event, (failure) => {
    const notice = failure.detail?.xhr?.getResponseHeader(
      "X-Mailspace-Decision-Notice",
    );
    window.dispatchEvent(
      new CustomEvent("mailspace:toast", {
        detail: {
          // Only this server-controlled diagnostic header is displayed, never
          // arbitrary response bodies which may contain private message data.
          message:
            notice?.slice(0, 600) ||
            "Unable to finish this action. Check the connection and try again.",
          kind: "error",
        },
      }),
    );
  });

document.addEventListener("htmx:afterSettle", (event) => {
  if (
    event.detail.target?.hasAttribute("data-reply-status") &&
    event.detail.target.querySelector("[data-outgoing]")
  ) {
    const form = event.detail.target.closest("form");
    if (form) {
      form.querySelector("textarea").value = "";
      for (const button of form.querySelectorAll("button"))
        button.disabled = true;
      form.addEventListener(
        "submit",
        (event) => {
          event.preventDefault();
          event.stopImmediatePropagation();
        },
        true,
      );
    }
  }
});

let commandMessage = null;
function prepareContextCommands() {
  const current = new URL(location.href);
  const thread = document.getElementById("thread-drawer")?.open
    ? new URL(
        document.querySelector("[data-thread-url]")?.dataset.threadUrl ||
          location.href,
        location.href,
      ).searchParams.get("thread")
    : current.searchParams.get("thread") ||
      (commandMessage?.isConnected ? commandMessage.dataset.messageKey : "");
  const person =
    current.searchParams.get("person") ||
    (commandMessage?.isConnected ? commandMessage.dataset.messagePerson : "");
  for (const link of document.querySelectorAll("[data-context-command]")) {
    const [action, scope] = link.dataset.contextCommand.split("-");
    const value = scope === "thread" ? thread : person;
    link.dataset.contextAvailable = value ? "true" : "false";
    link.hidden = !value;
    if (value)
      link.href =
        (action === "archive" ? "/archive/preview?" : "/labels/assign?") +
        new URLSearchParams({
          scope,
          [scope === "thread" ? "key" : "person"]: value,
        });
  }
}
for (const type of ["click", "focusin"])
  document.addEventListener(
    type,
    (event) => {
      const article = event.target.closest("[data-message-key]");
      if (article) commandMessage = article;
    },
    true,
  );
// Native dialogs provide focus containment, Escape and focus restoration.
document.addEventListener(
  "close",
  (event) => {
    if (event.target.id === "thread-drawer") {
      clearAttachments();
      cleanup();
      document.getElementById("thread-content")?.replaceChildren();
      initialize();
    }
  },
  true,
);
document.addEventListener("keydown", (event) => {
  if (event.isComposing || event.repeat || event.altKey) return;
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
    const menu = document.getElementById("command-menu");
    if (!menu) return;
    if (
      document.querySelector(
        "dialog[open]:not(#command-menu):not(#thread-drawer):not(#app-panel-dialog)",
      )
    )
      return;
    prepareContextCommands();
    event.preventDefault();
    if (menu.open) menu.close();
    else menu.showModal();
  }
});
document.addEventListener("input", (event) => {
  if (!event.target.matches("[data-command-search]")) return;
  const query = event.target.value.toLowerCase().trim();
  for (const item of document.querySelectorAll("[data-command]"))
    item.hidden =
      item.dataset.contextAvailable === "false" ||
      !item.textContent.toLowerCase().includes(query);
});
document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-load-attachments]");
  if (!button || button.disabled) return;
  const list = document.getElementById(button.getAttribute("aria-controls"));
  if (!list) return;
  const epoch = attachmentEpoch;
  button.disabled = true;
  button.setAttribute("aria-busy", "true");
  list.textContent = "Loading attachments…";
  const request = new AbortController();
  attachmentRequests.add(request);
  try {
    const [{ parseLocalMime }, { extractSigned }, { AttachmentViewer }] =
      await Promise.all([
        import("./mime.mjs"),
        import("./signed-mime.mjs"),
        import("./attachments.mjs"),
      ]);
    const response = await fetch(
      "/api/messages/raw?" +
        new URLSearchParams({ reference: button.dataset.reference }),
      {
        credentials: "same-origin",
        cache: "no-store",
        redirect: "error",
        signal: request.signal,
      },
    );
    if (!response.ok) throw Error("source unavailable");
    const raw = new Uint8Array(await response.arrayBuffer());
    const signed = await extractSigned(raw);
    const parsed = await parseLocalMime(signed?.data || raw);
    if (epoch !== attachmentEpoch || !button.isConnected) return;
    const viewer = new AttachmentViewer(list, document);
    viewer.triggerButton = button;
    viewer.triggerLabel = button.textContent;
    attachmentViewers.add(viewer);
    viewer.render(
      parsed.attachments.filter(
        (a) => a.mimeType !== "application/pgp-signature",
      ),
    );
    const notice = document.createElement("p");
    notice.className = "text-xs text-muted";
    notice.textContent =
      "Local attachment bytes. Open the full message for signature verification.";
    list.prepend(notice);
    button.setAttribute("aria-label", "Attachments loaded");
    button.title = "Attachments loaded";
  } catch {
    if (epoch === attachmentEpoch)
      list.textContent =
        "Attachments could not be loaded. Open Attachments / full message to try the local reader.";
    button.disabled = false;
  } finally {
    attachmentRequests.delete(request);
    button.removeAttribute("aria-busy");
  }
});
window.addEventListener("pagehide", clearAttachments);
window.addEventListener("mailspace:lock", clearAttachments);

document.addEventListener("htmx:afterRequest", (event) => {
  if (
    event.detail.target?.id === "thread-content" &&
    !event.detail.successful
  ) {
    const target = event.detail.target;
    const p = document.createElement("p");
    p.className = "p-6 text-sm text-error";
    p.textContent =
      "Thread could not be loaded. Close this drawer and try again; cached mail is unchanged.";
    target.replaceChildren(p);
  }
});

document.addEventListener("keydown", (event) => {
  const menu = document.getElementById("command-menu");
  if (
    !menu?.open ||
    event.isComposing ||
    event.ctrlKey ||
    event.metaKey ||
    event.altKey ||
    !["ArrowDown", "ArrowUp"].includes(event.key)
  )
    return;
  const target = event.target;
  if (
    target.matches("input,textarea") &&
    !target.matches("[data-command-search]")
  )
    return;
  const items = [
    ...menu.querySelectorAll(
      "a[data-command],button[data-command],form[data-command] button",
    ),
  ].filter((item) => !item.closest("[hidden]") && !item.disabled);
  if (!items.length) return;
  event.preventDefault();
  const current = items.indexOf(document.activeElement);
  const next =
    current < 0
      ? event.key === "ArrowDown"
        ? 0
        : items.length - 1
      : (current + (event.key === "ArrowDown" ? 1 : -1) + items.length) %
        items.length;
  items[next].focus();
});

window.addEventListener("mailspace:panel-closed", () => {
  const shell = document.getElementById("timeline-shell");
  if (
    !shell ||
    [...shell.querySelectorAll("[data-reply-body]")].some((el) =>
      el.value.trim(),
    )
  )
    return;
  const drawer = document.getElementById("thread-drawer");
  const url = drawer?.open
    ? document.querySelector("[data-thread-url]")?.dataset.threadUrl
    : shell.dataset.feedUrl;
  const target = drawer?.open ? "#thread-content" : "#timeline-shell";
  if (url && window.htmx)
    window.htmx.ajax("GET", url, {
      target,
      swap: drawer?.open ? "innerHTML" : "outerHTML",
    });
});

window.addEventListener("mailspace:sent", (event) => {
  window.dispatchEvent(
    new CustomEvent("mailspace:toast", {
      detail: {
        message:
          event.detail?.state === "accepted"
            ? "Mail server accepted this message."
            : "Send outcome needs attention. Check Outbox before sending again.",
        href: event.detail?.location,
      },
    }),
  );
  const shell = document.getElementById("timeline-shell");
  const drawer = document.getElementById("thread-drawer");
  const source = drawer?.open
    ? document.querySelector("[data-thread-url]")?.dataset.threadUrl
    : shell?.dataset.feedUrl;
  if (!source || !window.htmx) return;
  const url = new URL(source, location.href);
  url.searchParams.delete("reply");
  url.searchParams.delete("compose");
  url.hash = "";
  window.htmx.ajax("GET", url.pathname + url.search, {
    target: drawer?.open ? "#thread-content" : "#timeline-shell",
    swap: drawer?.open ? "innerHTML" : "outerHTML",
  });
});
