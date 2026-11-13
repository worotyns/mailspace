// SPDX-License-Identifier: AGPL-3.0-only
import { LocalVault, VaultError } from "./vault.mjs";
import { VaultStore } from "./store.mjs";
import { composeEntity, encryptedEntity } from "./mime.mjs";
import { addressParser } from "../vendor/postal-mime/postal-mime.js";
export class InlineEncryption {
  constructor(shell, form) {
    if (!isSecureContext || !crypto.subtle || !indexedDB)
      throw new Error("secure context required");
    this.shell = shell;
    this.form = form;
    this.vault = new LocalVault(new VaultStore(shell.dataset.userId));
    this.dialog = shell.querySelector("#inline-key-dialog");
    this.select = this.dialog.querySelector("select");
    this.pass = this.dialog.querySelector("input");
    this.keyStatus = this.dialog.querySelector("#inline-key-status");
    this.status = form.querySelector("[data-reply-status]");
    this.body = form.querySelector("textarea");
    this.button = form.querySelector("[data-send]");
    this.events = new AbortController();
    const options = { signal: this.events.signal };
    this.dialog
      .querySelector("#inline-key-unlock")
      .addEventListener("click", () => this.unlock(), options);
    this.dialog.addEventListener(
      "close",
      () => {
        this.pass.value = "";
      },
      options,
    );
    window.addEventListener("mailspace:lock", () => this.lock(), options);
    document.addEventListener(
      "visibilitychange",
      () => {
        if (document.hidden) this.lock();
      },
      options,
    );
    for (const name of ["keydown", "pointerdown"])
      document.addEventListener(
        name,
        () => {
          this.activity = Date.now();
        },
        options,
      );
    this.activity = Date.now();
    this.epoch = 0;
    this.timer = setInterval(() => {
      if (
        Date.now() >= Number(shell.dataset.sessionExpires) ||
        (!this.vault.locked && Date.now() - this.activity > 300000)
      )
        this.lock();
    }, 1000);
  }
  check() {
    if (
      this.disposed ||
      !Number.isFinite(Number(this.shell.dataset.sessionExpires)) ||
      Date.now() >= Number(this.shell.dataset.sessionExpires)
    )
      throw new VaultError("locked");
  }
  async open() {
    this.check();
    const epoch = this.epoch;
    const snapshot = await this.vault.list();
    if (epoch !== this.epoch || this.disposed) return;
    this.select.replaceChildren();
    for (const key of snapshot.keys.filter((key) => key.private)) {
      const option = document.createElement("option");
      option.value = key.fingerprint;
      option.textContent = `${key.identities.join(" · ")} · ${key.fingerprint}`;
      this.select.append(option);
    }
    this.keyStatus.textContent = this.select.options.length
      ? "Unlock your key. Sending stays a separate action."
      : "No local private key. Generate or import one in Local keys, then verify recipient fingerprints.";
    this.dialog.querySelector("#inline-key-unlock").disabled =
      !this.select.options.length;
    this.dialog.showModal();
  }
  async unlock() {
    if (this.busy) return;
    this.busy = true;
    const epoch = this.epoch;
    try {
      this.check();
      await this.vault.unlock(this.select.value, this.pass.value);
      if (epoch !== this.epoch) return;
      this.activity = Date.now();
      this.status.textContent =
        "Key unlocked locally. Ready to encrypt; recipient trust is checked before sending.";
      this.dialog.close();
    } catch {
      if (epoch === this.epoch)
        this.keyStatus.textContent =
          "Could not unlock this key. Check the key and passphrase.";
    } finally {
      this.pass.value = "";
      this.busy = false;
    }
  }
  lock(clear = true) {
    this.epoch++;
    this.vault.lock();
    this.request?.abort();
    this.pass.value = "";
    this.dialog.close();
    if (clear) this.body.value = "";
  }
  async send() {
    if (this.busy || this.submitted) return;
    this.check();
    if (this.vault.locked) {
      await this.open();
      return;
    }
    this.busy = true;
    this.button.disabled = true;
    const epoch = this.epoch;
    try {
      const value = (name) => this.form.elements.namedItem(name)?.value || "";
      const to = value("to");
      if (to.length > 8192 || /[\r\n\0]/.test(to))
        throw new VaultError("invalid-recipients");
      const recipients = addressParser(to, { flatten: true }).map(
        (item) => item.address,
      );
      if (
        !recipients.length ||
        recipients.length > 100 ||
        recipients.some(
          (address) =>
            !address || !address.includes("@") || /[^\x21-\x7e]/.test(address),
        )
      )
        throw new VaultError("invalid-recipients");
      const account = this.form.elements.namedItem("account");
      const from =
        this.form.dataset.from || account.selectedOptions?.[0]?.dataset.address;
      const subject = this.form.dataset.subject || value("subject");
      if (
        /[\r\n\0]/.test(subject) ||
        new TextEncoder().encode(subject).length > 200
      )
        throw new VaultError("subject-limit");
      const ciphertext = await this.vault.encryptSigned(
        composeEntity(this.body.value, []),
        from,
        recipients,
      );
      if (epoch !== this.epoch) return;
      this.check();
      // Explicit allowlist; no FormData, body, key, passphrase or decrypted MIME.
      const payload = {
        account: value("account"),
        requestId: value("request_id"),
        reference: value("reference"),
        space: value("space"),
        to,
        cc: "",
        bcc: "",
        subject,
        encryptedMime: encryptedEntity(ciphertext),
      };
      this.submitted = true;
      this.request = new AbortController();
      this.status.textContent =
        "Submitting ciphertext once. Check Outbox if the result is interrupted.";
      const response = await fetch("/api/outbox/encrypted", {
        method: "POST",
        credentials: "same-origin",
        redirect: "error",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": this.shell.dataset.csrf,
        },
        body: JSON.stringify(payload),
        signal: this.request.signal,
      });
      if (!response.ok) throw new Error("submission uncertain");
      const result = await response.json();
      if (epoch !== this.epoch) return;
      if (
        typeof result.location !== "string" ||
        !result.location.startsWith("/outbox?")
      )
        throw new Error("invalid result");
      this.body.value = "";
      location.assign(result.location);
    } catch (error) {
      if (epoch !== this.epoch) return;
      this.status.textContent = this.submitted
        ? "Send result uncertain. Open Outbox before any new send; this composer will not submit twice."
        : [
              "untrusted-recipient",
              "key-changed",
              "sender-key-mismatch",
              "unusable-key",
            ].includes(error.code)
          ? "A verified recipient or sender key is missing, changed or unusable. Check fingerprints in Local keys. No message was sent."
          : "Could not encrypt. Check recipients, subject and local keys. No plaintext was uploaded.";
    } finally {
      this.busy = false;
      this.button.disabled = Boolean(this.submitted);
    }
  }
  dispose() {
    this.disposed = true;
    this.lock();
    clearInterval(this.timer);
    this.events.abort();
    void this.vault.close();
  }
}
