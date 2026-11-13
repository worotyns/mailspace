// SPDX-License-Identifier: AGPL-3.0-only
// Attachment bytes never leave the local reader. No HTML, SVG or PDF execution.
export function previewKind(attachment) {
  const bytes = new Uint8Array(attachment.content);
  if (
    bytes.length <= 4 << 20 &&
    bytes.length >= 33 &&
    [137, 80, 78, 71, 13, 10, 26, 10].every(
      (value, index) => bytes[index] === value,
    )
  ) {
    if (
      ![0, 0, 0, 13, 73, 72, 68, 82].every(
        (value, index) => bytes[index + 8] === value,
      )
    )
      return null;
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    const width = view.getUint32(16),
      height = view.getUint32(20);
    if (
      width > 0 &&
      height > 0 &&
      width <= 4096 &&
      height <= 4096 &&
      width * height <= 16_000_000
    )
      return "png";
  }
  if (
    bytes.length <= 256 << 10 &&
    /^(text\/plain|application\/json)$/.test(attachment.mimeType || "")
  ) {
    try {
      const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
      if (!text.includes("\0")) return "text";
    } catch {}
  }
  return null;
}
export class AttachmentViewer {
  constructor(container, document) {
    this.container = container;
    this.document = document;
    this.urls = [];
    this.dialog = document.createElement("dialog");
    this.dialog.className =
      "m-auto max-h-[90dvh] w-[94vw] max-w-3xl overflow-y-auto rounded-2xl border border-line bg-panel p-5 text-ink backdrop:bg-black/50";
    this.dialog.setAttribute("aria-label", "Local attachment preview");
    this.close = document.createElement("button");
    this.close.type = "button";
    this.close.textContent = "Close attachment preview";
    this.close.autofocus = true;
    this.close.className = "mb-4 rounded-lg border border-line px-3 py-2";
    this.close.addEventListener("click", () => this.dialog.close());
    this.content = document.createElement("div");
    this.dialog.append(this.close, this.content);
    document.body.append(this.dialog);
    this.dialog.addEventListener("close", () => this.content.replaceChildren());
  }
  render(attachments) {
    this.clear();
    if (!attachments.length) {
      const p = this.document.createElement("p");
      p.textContent = "No attachments in this message.";
      p.className = "text-xs text-muted";
      this.container.append(p);
      return;
    }
    const heading = this.document.createElement("h3");
    heading.textContent = `Attachments (${attachments.length})`;
    heading.className = "font-medium";
    this.container.append(heading);
    for (const attachment of attachments) {
      const name = (attachment.filename || "attachment.bin")
        .replace(/[\/\\\x00-\x1f]/g, "_")
        .slice(0, 180);
      const bytes = new Uint8Array(attachment.content);
      const url = URL.createObjectURL(
        new Blob([bytes], { type: "application/octet-stream" }),
      );
      this.urls.push(url);
      const row = this.document.createElement("div");
      row.className =
        "rounded-lg border border-line p-3 [overflow-wrap:anywhere]";
      const link = this.document.createElement("a");
      link.href = url;
      link.download = name;
      link.textContent = `Download local attachment: ${name} (${bytes.length} bytes)`;
      link.className = "block text-sm text-accent";
      row.append(link);
      const kind = previewKind(attachment);
      if (kind) {
        const button = this.document.createElement("button");
        button.type = "button";
        button.textContent = "Preview attachment";
        button.className =
          "mt-2 rounded-lg border border-line px-3 py-2 text-xs";
        button.addEventListener("click", () => {
          this.content.replaceChildren();
          if (kind === "text") {
            const pre = this.document.createElement("pre");
            pre.className = "whitespace-pre-wrap break-words text-sm";
            pre.textContent = new TextDecoder().decode(bytes);
            this.content.append(pre);
          } else {
            const image = this.document.createElement("img");
            const imageURL = URL.createObjectURL(
              new Blob([bytes], { type: "image/png" }),
            );
            this.urls.push(imageURL);
            image.src = imageURL;
            image.alt = name;
            image.className = "max-h-[70dvh] max-w-full object-contain";
            this.content.append(image);
          }
          this.dialog.showModal();
        });
        row.append(button);
      } else {
        const note = this.document.createElement("p");
        note.className = "mt-1 text-xs text-muted";
        note.textContent =
          "Download available. Preview supports UTF-8 text/JSON and bounded PNG images.";
        row.append(note);
      }
      this.container.append(row);
    }
  }
  dispose() {
    this.clear();
    this.dialog.remove();
  }
  clear() {
    if (this.dialog.open) this.dialog.close();
    this.content.replaceChildren();
    this.container.replaceChildren();
    for (const url of this.urls) URL.revokeObjectURL(url);
    this.urls = [];
  }
}
