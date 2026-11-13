// SPDX-License-Identifier: AGPL-3.0-only
// Plain replies use HTMX. Encrypted mode has no serializable body field and
// intercepts submission before HTMX; the crypto module uploads ciphertext only.
let cleanup = () => {};
function initialize() {
  cleanup();
  const shell = document.getElementById("timeline-shell");
  if (!shell) return;
  const feed = shell.querySelector("#timeline-feed");
  if (feed?.dataset.jumpLatest.trim() === "true")
    feed.scrollTop = feed.scrollHeight;
  const selected = shell.querySelector('[data-selected="true"]');
  if (feed && selected)
    feed.scrollTop +=
      selected.getBoundingClientRect().top -
      feed.getBoundingClientRect().top -
      feed.clientHeight / 3;
  const form = shell.querySelector("#inline-reply-form");
  if (!form) return;
  const body = form.querySelector("textarea"),
    toggle = form.querySelector("#inline-encrypt-toggle"),
    send = form.querySelector("#inline-send"),
    status = form.querySelector("#inline-reply-status"),
    state = form.querySelector("#inline-encryption-state");
  const required = form.dataset.encryptionRequired === "true";
  let encrypted = required,
    controller,
    disposed = false,
    pending;
  function paint() {
    toggle.setAttribute("aria-pressed", String(encrypted));
    toggle.textContent = encrypted ? "🔒 Encryption on" : "Encrypt";
    state.textContent = encrypted
      ? "Encrypted locally before sending"
      : "No encryption selected";
    send.textContent = encrypted ? "Send encrypted" : "Send unencrypted";
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
  toggle.addEventListener("click", async () => {
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
  });
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
    true,
  );
  // Defense against programmatic HTMX triggers, including already-bound forms.
  form.addEventListener("htmx:configRequest", (event) => {
    if (encrypted) event.preventDefault();
  });
  cleanup = () => {
    disposed = true;
    controller?.dispose();
  };
  paint();
}
document.addEventListener("click", (event) => {
  const opener = event.target.closest("[data-open-dialog]");
  if (opener) {
    const dialog = document.getElementById(opener.dataset.openDialog);
    if (dialog?.showModal) {
      event.preventDefault();
      dialog.showModal();
    }
  }
  const close = event.target.closest("[data-close-dialog]");
  if (close) close.closest("dialog")?.close();
});
document.addEventListener("htmx:beforeSwap", (event) => {
  if (event.detail.target?.id === "timeline-shell") cleanup();
  if (
    event.detail.xhr?.status === 422 &&
    event.detail.target?.id === "inline-reply-status"
  ) {
    event.detail.shouldSwap = true;
    event.detail.isError = false;
  }
});
document.addEventListener("htmx:afterSwap", (event) => {
  if (event.detail.target?.id === "timeline-shell") initialize();
});
window.addEventListener("pagehide", () => cleanup());
initialize();

document.addEventListener("htmx:beforeRequest", () => {
  document.getElementById("request-error").hidden = true;
});
for (const event of ["htmx:responseError", "htmx:sendError", "htmx:timeout"])
  document.addEventListener(event, () => {
    document.getElementById("request-error").hidden = false;
  });

document.addEventListener("htmx:afterSettle", (event) => {
  if (
    event.detail.target?.id === "inline-reply-status" &&
    event.detail.target.querySelector("[data-outgoing]")
  ) {
    const form = document.getElementById("inline-reply-form");
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
