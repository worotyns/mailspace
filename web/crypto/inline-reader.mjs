// SPDX-License-Identifier: AGPL-3.0-only
// Inline decrypted content is DOM-only, hx-disabled and never serialized.
import { renderReadableText } from "./message-text.mjs";
import { createSessionVault } from "./session-vault.mjs";
import { extractEncrypted, parseLocalMime } from "./mime.mjs";
import { extractSigned } from "./signed-mime.mjs";
import { messageText } from "./mail-html.mjs";
import { AttachmentViewer } from "./attachments.mjs";
let preference = false;
let dialogNumber = 0;

export function renderQuotedText(container, text, document) {
  renderReadableText(container, text, document);
}

function signatureText(signatures = []) {
  if (!signatures.length)
    return "Unsigned message · sender identity is not verified";
  return signatures
    .map((signature) =>
      signature.status === "valid"
        ? signature.trustedSender
          ? "Valid signature · verified sender fingerprint"
          : "Valid signature · sender identity not trusted"
        : signature.status === "invalid"
          ? "INVALID signature · do not trust this message"
          : "Unknown signing key · signature not verified",
    )
    .join("; ");
}
export class InlineReader {
  constructor(shell, options = {}) {
    this.shell = shell;
    this.document = shell.ownerDocument;
    this.window = this.document.defaultView;
    this.vault = (options.vaultFactory || createSessionVault)(
      shell.dataset.userId,
    );
    this.fetch = options.fetch || this.window.fetch.bind(this.window);
    this.extractEncrypted = options.extractEncrypted || extractEncrypted;
    this.parseLocalMime = options.parseLocalMime || parseLocalMime;
    this.canDecrypt = options.canDecrypt ?? this.window.isSecureContext;
    this.epoch = 0;
    this.requests = new Set();
    this.busy = new Map();
    this.viewers = new Map();
    this.queue = [];
    this.attempted = new Set();
    this.active = 0;
    this.lastActivity = Date.now();
    this.lastTouch = 0;
    this.automatic = options.automatic ?? preference;
    this.events = new this.window.AbortController();
    const signal = this.events.signal;
    this.createDialog();
    this.entries = [...shell.querySelectorAll("[data-inline-encrypted]")];
    this.controls = [
      ...this.document.querySelectorAll("[data-inline-reader-controls]"),
    ];
    for (const control of this.controls) control.hidden = false;
    for (const input of this.document.querySelectorAll(
      "[data-local-autopreview]",
    ))
      input.checked = this.automatic;
    this.document.addEventListener(
      "click",
      (event) => {
        const trigger = event.target.closest("[data-inline-decrypt]");
        if (
          trigger &&
          shell.contains(trigger) &&
          !event.metaKey &&
          !event.ctrlKey &&
          !event.shiftKey &&
          !event.altKey &&
          event.button === 0 &&
          this.canDecrypt
        ) {
          event.preventDefault();
          void this.preview(trigger.closest("[data-inline-encrypted]"));
        }
        if (event.target.closest("[data-local-key-lock]")) {
          this.vault.lock();
          this.clearPrivate();
          this.announce(
            "Local key session locked. Decrypted previews cleared.",
          );
        }
      },
      { signal },
    );
    for (const type of ["keydown", "pointerdown"])
      this.document.addEventListener(
        type,
        () => {
          this.lastActivity = Date.now();
          if (
            !this.vault.locked &&
            this.lastActivity - this.lastTouch >= 15000
          ) {
            this.lastTouch = this.lastActivity;
            void this.vault.touch?.().catch(() => this.clearPrivate());
          }
        },
        { passive: true, signal },
      );
    this.document.addEventListener(
      "change",
      (event) => {
        if (!event.target.matches("[data-local-autopreview]")) return;
        this.automatic = preference = event.target.checked;
        this.attempted.clear();
        if (this.automatic) void this.refreshAutomatic();
        else this.queue = [];
      },
      { signal },
    );
    this.document.addEventListener(
      "visibilitychange",
      () => {
        if (this.document.hidden) this.clearPrivate();
        else if (this.automatic) void this.refreshAutomatic();
      },
      { signal },
    );
    this.document.addEventListener(
      "close",
      () => {
        if (this.automatic) void this.refreshAutomatic();
      },
      { capture: true, signal },
    );
    this.window.addEventListener(
      "mailspace:lock",
      () => {
        this.vault.lock();
        this.clearPrivate();
      },
      { signal },
    );
    this.window.addEventListener(
      "mailspace:panel-closed",
      () => this.clearPrivate(),
      { signal },
    );
    this.vault.addEventListener?.("lock", () => this.clearPrivate(), {
      signal,
    });
    this.unsubscribe = this.vault.onStateChange?.((state) => {
      if (this.disposed) return;
      this.paintState(state);
      if (!state.locked && this.automatic) this.queueVisible();
    });
    const Observer =
      options.IntersectionObserver || this.window.IntersectionObserver;
    if (Observer) {
      this.observer = new Observer((entries) => {
        if (!this.automatic || this.vault.locked) return;
        for (const entry of entries)
          if (entry.isIntersecting) this.enqueue(entry.target);
        this.drain();
      });
      for (const entry of this.entries) this.observer.observe(entry);
    } else
      this.document.addEventListener("scroll", () => this.queueVisible(), {
        capture: true,
        passive: true,
        signal,
      });
    this.timer = this.window.setInterval(() => {
      if (this.expired) return;
      if (
        Date.now() >= Number(this.shell.dataset.sessionExpires) ||
        (!this.vault.locked && Date.now() - this.lastActivity > 300000)
      ) {
        this.expired = Date.now() >= Number(this.shell.dataset.sessionExpires);
        this.vault.lock();
        this.clearPrivate();
      }
    }, 1000);
    if (this.automatic) void this.refreshAutomatic();
    else
      void this.vault
        .list()
        .then((state) => {
          if (!this.disposed) this.paintState(state);
        })
        .catch(() => {});
  }
  announce(message) {
    this.window.dispatchEvent(
      new this.window.CustomEvent("mailspace:toast", { detail: { message } }),
    );
  }
  paintState(state) {
    const mode =
      this.vault.sessionMode === "shared"
        ? "Shared browser memory session"
        : this.vault.sessionMode === "dedicated"
          ? "This page’s worker session"
          : "This page’s memory session";
    const text =
      state.locked || this.vault.locked
        ? "Local key locked · use Preview locally to unlock"
        : `${mode} · key unlocked · locks after 5 minutes of inactivity`;
    for (const label of this.document.querySelectorAll(
      "[data-local-key-state]",
    ))
      label.textContent = text;
  }
  valid(epoch) {
    return (
      !this.disposed &&
      epoch === this.epoch &&
      this.shell.isConnected &&
      !this.document.hidden &&
      Date.now() < Number(this.shell.dataset.sessionExpires)
    );
  }
  visible(entry) {
    if (!entry?.isConnected || this.document.hidden) return false;
    const drawer = this.shell.querySelector("#thread-drawer[open]");
    if (drawer && !drawer.contains(entry)) return false;
    if (this.document.querySelector("dialog[open]:not(#thread-drawer)"))
      return false;
    const rect = entry.getBoundingClientRect();
    return (
      rect.width > 0 &&
      rect.height > 0 &&
      rect.bottom > 0 &&
      rect.top < this.window.innerHeight
    );
  }
  async refreshAutomatic() {
    if (this.disposed) return;
    try {
      const epoch = this.epoch;
      const state = await this.vault.list();
      if (!this.valid(epoch)) return;
      this.paintState(state);
      if (!state.locked && this.automatic) this.queueVisible();
    } catch {
      /* Automatic preview stays paused; a manual action shows feedback. */
    }
  }
  queueVisible() {
    if (!this.automatic || this.vault.locked) return;
    for (const entry of this.entries)
      if (this.visible(entry)) this.enqueue(entry);
    this.drain();
  }
  enqueue(entry) {
    if (
      !this.visible(entry) ||
      this.busy.has(entry) ||
      this.attempted.has(entry)
    )
      return;
    this.attempted.add(entry);
    this.queue.push(entry);
  }
  drain() {
    while (
      this.automatic &&
      !this.vault.locked &&
      this.active < 2 &&
      this.queue.length
    ) {
      const entry = this.queue.shift();
      this.active++;
      void this.preview(entry, true).finally(() => {
        this.active--;
        this.drain();
      });
    }
  }
  createDialog() {
    const doc = this.document,
      id = `inline-reader-unlock-${++dialogNumber}`;
    this.dialog = doc.createElement("dialog");
    this.dialog.className =
      "m-auto w-[94vw] max-w-md rounded-2xl border border-line bg-panel p-5 text-ink backdrop:bg-black/40";
    this.dialog.setAttribute("aria-labelledby", id);
    this.dialog.setAttribute("hx-disable", "");
    const heading = doc.createElement("h2");
    heading.id = id;
    heading.textContent = "Unlock a local key";
    heading.className = "font-semibold";
    const hint = doc.createElement("p");
    hint.className = "mt-2 text-xs text-muted";
    hint.textContent =
      "Your key stays in browser memory for up to 5 minutes of inactivity. It never goes to the server. Browser restart or reload may require unlocking again.";
    const keyLabel = doc.createElement("label");
    keyLabel.textContent = "Private key";
    keyLabel.className = "mt-4 block text-sm";
    this.select = doc.createElement("select");
    this.select.className =
      "mt-1 block w-full rounded-lg border border-line bg-canvas px-3";
    keyLabel.append(this.select);
    const passLabel = doc.createElement("label");
    passLabel.textContent = "Key passphrase";
    passLabel.className = "mt-3 block text-sm";
    this.pass = doc.createElement("input");
    this.pass.type = "password";
    this.pass.autocomplete = "off";
    this.pass.autofocus = true;
    this.pass.className =
      "mt-1 block w-full rounded-lg border border-line bg-canvas px-3";
    passLabel.append(this.pass);
    this.keyStatus = doc.createElement("p");
    this.keyStatus.className = "mt-3 text-xs text-muted";
    this.keyStatus.setAttribute("role", "status");
    const actions = doc.createElement("div");
    actions.className = "mt-4 flex gap-3";
    this.unlockButton = doc.createElement("button");
    this.unlockButton.type = "button";
    this.unlockButton.textContent = "Unlock & preview";
    this.unlockButton.className =
      "rounded-lg bg-action px-3 py-2 text-sm text-white";
    this.unlockButton.addEventListener("click", () => void this.unlock());
    const cancel = doc.createElement("button");
    cancel.type = "button";
    cancel.textContent = "Cancel";
    cancel.className = "rounded-lg border border-line px-3 py-2 text-sm";
    cancel.addEventListener("click", () => {
      this.cancelUnlock();
      this.dialog.close();
    });
    actions.append(this.unlockButton, cancel);
    this.dialog.append(
      heading,
      hint,
      keyLabel,
      passLabel,
      this.keyStatus,
      actions,
    );
    this.dialog.addEventListener("cancel", () => this.cancelUnlock());
    this.dialog.addEventListener("close", () => {
      if (this.dialog.returnValue !== "unlocked") this.cancelUnlock();
      this.dialog.returnValue = "";
      this.pass.value = "";
      this.pendingEntry = null;
    });
    doc.body.append(this.dialog);
  }
  showUnlock(entry, state) {
    this.pendingEntry = entry;
    this.select.replaceChildren();
    for (const key of state.keys.filter((key) => key.private)) {
      const option = this.document.createElement("option");
      option.value = key.fingerprint;
      option.textContent = `${key.identities.join(" · ")} · ${key.fingerprint.slice(-16)}`;
      this.select.append(option);
    }
    this.unlockButton.disabled = !this.select.options.length;
    this.keyStatus.textContent = this.select.options.length
      ? "Unlocking does not send a message or share its content."
      : "Import your private key in Settings → Keys, then open preview again.";
    if (!this.dialog.open) this.dialog.showModal();
  }
  cancelUnlock() {
    if (this.unlocking && !this.unlockCancelled) {
      this.unlockCancelled = true;
      this.epoch++;
      this.vault.lock();
    }
  }
  async unlock() {
    if (this.unlocking || !this.pendingEntry) return;
    this.unlocking = true;
    this.unlockCancelled = false;
    this.unlockButton.disabled =
      this.select.disabled =
      this.pass.disabled =
        true;
    const epoch = this.epoch,
      entry = this.pendingEntry;
    try {
      await this.vault.unlock(this.select.value, this.pass.value);
      if (!this.valid(epoch)) return;
      this.lastActivity = Date.now();
      this.dialog.close("unlocked");
      this.attempted.clear();
      await this.preview(entry);
      if (this.automatic) this.queueVisible();
    } catch {
      if (this.valid(epoch))
        this.keyStatus.textContent =
          "Could not unlock this key. Check the passphrase and current login session.";
    } finally {
      this.pass.value = "";
      this.unlocking = false;
      this.select.disabled = this.pass.disabled = false;
      this.unlockButton.disabled = !this.select.options.length;
    }
  }
  async preview(entry, automatic = false) {
    if (!entry || this.busy.has(entry) || this.disposed) return;
    if (automatic && !this.visible(entry)) return;
    const trigger = entry.querySelector("[data-inline-decrypt]"),
      output = entry.querySelector("[data-local-preview]");
    if (!trigger || !output) return;
    const epoch = this.epoch;
    const job = {};
    this.busy.set(entry, job);
    trigger.setAttribute("aria-disabled", "true");
    trigger.setAttribute("aria-busy", "true");
    trigger.textContent = "Decrypting…";
    let request;
    try {
      const state = await this.vault.list();
      if (!this.valid(epoch) || !entry.isConnected) return;
      this.paintState(state);
      if (state.locked) {
        if (!automatic) this.showUnlock(entry, state);
        return;
      }
      request = new AbortController();
      this.requests.add(request);
      const token = entry.closest("[data-message-reference]")?.dataset
        .messageReference;
      if (!token) throw new Error("source unavailable");
      const response = await this.fetch(
        "/api/messages/raw?" + new URLSearchParams({ reference: token }),
        {
          credentials: "same-origin",
          cache: "no-store",
          redirect: "error",
          signal: request.signal,
        },
      );
      if (!response.ok) throw new Error("source unavailable");
      const bytes = new Uint8Array(await response.arrayBuffer());
      if (bytes.length > 8 << 20) throw new Error("message limit");
      let result = await this.vault.decryptSigned(
        await this.extractEncrypted(bytes),
        entry.closest("[data-message-person]")?.dataset.messagePerson || "",
      );
      if (result.provenance !== "client-decrypted")
        throw new Error("invalid provenance");
      const signed = await extractSigned(result.data);
      if (signed) {
        const verified = await this.vault.verifyDetached(
          signed.data,
          signed.signature,
          entry.closest("[data-message-person]")?.dataset.messagePerson || "",
        );
        result = {
          ...result,
          data: verified.data,
          signatures: [...(result.signatures || []), ...verified.signatures],
        };
      }
      const parsed = await this.parseLocalMime(result.data);
      if (!this.valid(epoch) || !entry.isConnected || this.vault.locked) return;
      this.viewers.get(entry)?.dispose();
      const text = this.document.createElement("div");
      renderQuotedText(text, messageText(parsed), this.document);
      const signature = this.document.createElement("p");
      signature.className = "mt-2 text-xs text-muted";
      signature.textContent = signatureText(result.signatures);
      const attachments = this.document.createElement("div");
      attachments.className = "mt-2";
      const viewer = new AttachmentViewer(attachments, this.document);
      viewer.render(
        parsed.attachments.filter(
          (item) => item.mimeType !== "application/pgp-signature",
        ),
      );
      this.viewers.set(entry, viewer);
      output.replaceChildren(text, signature, attachments);
      output.hidden = false;
      trigger.textContent = "Refresh local preview";
    } catch {
      if (this.valid(epoch) && !automatic)
        this.announce(
          "Could not preview this encrypted message. Check your local key and login session, or open the full message. No content was uploaded.",
        );
    } finally {
      this.requests.delete(request);
      if (this.busy.get(entry) === job) {
        this.busy.delete(entry);
        trigger.removeAttribute("aria-busy");
        trigger.removeAttribute("aria-disabled");
        if (output.hidden) trigger.textContent = "Preview locally";
      }
    }
  }
  clearPrivate() {
    this.epoch++;
    for (const request of this.requests) request.abort();
    this.requests.clear();
    this.busy.clear();
    this.queue = [];
    this.attempted.clear();
    for (const viewer of this.viewers.values()) viewer.dispose();
    this.viewers.clear();
    for (const entry of this.entries) {
      const output = entry.querySelector("[data-local-preview]");
      output?.replaceChildren();
      if (output) output.hidden = true;
      const trigger = entry.querySelector("[data-inline-decrypt]");
      if (trigger) {
        trigger.textContent = "Preview locally";
        trigger.removeAttribute("aria-busy");
        trigger.removeAttribute("aria-disabled");
      }
    }
    this.dialog.close();
    this.pass.value = "";
  }
  dispose() {
    this.disposed = true;
    this.clearPrivate();
    this.events.abort();
    this.unsubscribe?.();
    this.observer?.disconnect();
    this.window.clearInterval(this.timer);
    this.dialog.remove();
    void this.vault.close();
    for (const control of this.controls) control.hidden = true;
  }
}
