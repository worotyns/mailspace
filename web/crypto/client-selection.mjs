// SPDX-License-Identifier: AGPL-3.0-only
// Deliberate, per-excerpt client-decrypted export exception (ADR-0021).
// No named private controls, HTMX payloads, persistence, autosend or generic export.
export class ClientSelection {
  constructor(root) {
    this.root = root;
    const el = (id) => document.getElementById(id);
    this.dialog = el("selection-classify-dialog");
    this.excerpt = el("selection-classify-excerpt");
    this.consent = el("selection-classify-consent");
    this.status = el("selection-classify-status");
    this.result = el("selection-classify-result");
    this.button = el("selection-classify-send");
    this.openButton = el("selection-classify-open");
    this.epoch = 0;
    this.openButton.addEventListener("click", () => this.open());
    el("selection-classify-close").addEventListener("click", () =>
      this.dialog.close(),
    );
    this.dialog.addEventListener("close", () => this.clear(false));
    this.button.addEventListener("click", () => this.send());
  }
  session() {
    const expires = Number(this.root.dataset.sessionExpires);
    if (!Number.isFinite(expires) || Date.now() >= expires) {
      this.clear();
      window.dispatchEvent(new Event("mailspace:lock"));
      return false;
    }
    return true;
  }
  enable() {
    this.openButton.disabled = false;
  }
  open() {
    this.clear(false);
    if (!this.session()) return;
    const selected = window.getSelection?.();
    const text = document.getElementById("decrypted-text");
    // No full-message fallback. A missing selection starts with an empty editor.
    if (
      selected?.anchorNode &&
      selected.focusNode &&
      text.contains(selected.anchorNode) &&
      text.contains(selected.focusNode)
    )
      this.excerpt.value = selected.toString();
    this.requestId = crypto.randomUUID();
    this.dialog.showModal();
    this.excerpt.focus();
  }
  clear(close = true) {
    this.epoch++;
    this.request?.abort();
    this.excerpt.value = "";
    this.consent.checked = false;
    this.result.replaceChildren();
    this.status.textContent = "";
    this.button.disabled = false;
    this.excerpt.readOnly = false;
    this.submitted = false;
    if (close) {
      this.openButton.disabled = true;
      this.dialog.close();
    }
  }
  async send() {
    if (this.submitted) return;
    if (!this.session()) return;
    const bytes = new TextEncoder().encode(this.excerpt.value).length;
    if (!this.consent.checked || bytes < 1 || bytes > 4096) {
      this.status.textContent =
        "Review an excerpt up to 4 KiB and explicitly approve this one request.";
      return;
    }
    const epoch = this.epoch;
    this.submitted = true;
    this.button.disabled = true;
    this.excerpt.readOnly = true;
    this.request = new AbortController();
    const payload = {
      reference: this.root.dataset.classifyReference,
      excerpt: this.excerpt.value,
      requestId: this.requestId,
      consent: {
        provider: this.root.dataset.classifyProvider,
        purpose: "classify-selected-excerpt",
        approved: true,
      },
    };
    this.status.textContent =
      "Sharing only the approved excerpt for one proposal. No assignment happens automatically.";
    document.getElementById("secure-status").textContent =
      "A selected excerpt was explicitly shared for classification. Keys and the rest of this message remain local.";
    try {
      const response = await fetch("/api/automation/client-selection", {
        method: "POST",
        credentials: "same-origin",
        redirect: "error",
        cache: "no-store",
        headers: {
          "Content-Type": "application/json",
          "X-CSRF-Token": this.root.dataset.csrf,
        },
        body: JSON.stringify(payload),
        signal: this.request.signal,
      });
      if (epoch !== this.epoch) return;
      this.excerpt.value = "";
      if (!response.ok) throw new Error("classification failed");
      const result = await response.json();
      if (epoch !== this.epoch) return;
      if (!this.session()) return;
      if (
        !/^[A-Z2-7]{26}$/.test(result.id) ||
        !Array.isArray(result.spaces) ||
        result.spaces.length > 32
      )
        throw new Error("invalid proposal");
      for (const space of result.spaces) {
        const item = document.createElement("p");
        item.textContent = `Suggested Space: ${String(space.name)}`;
        this.result.append(item);
      }
      const link = document.createElement("a");
      link.href = `/automation#run-${result.id}`;
      link.textContent = "Review and approve this proposal";
      link.className = "block font-medium text-accent";
      this.result.append(link);
      this.status.textContent =
        "Excerpt cleared. Only decision references were retained. Review the proposal to assign this message; no model output grants access or sends mail.";
    } catch {
      if (epoch === this.epoch) {
        this.excerpt.value = "";
        this.status.textContent =
          "The request could not finish. Check Space automation and provider settings. No automatic retry or assignment happened.";
      }
    }
  }
}
