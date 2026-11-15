// SPDX-License-Identifier: AGPL-3.0-only
// Attachment bytes never leave the local reader. No HTML, SVG or PDF execution.
import { controlIcon } from "./icons.mjs";
function boundedImage(width, height) {
  return (
    width > 0 &&
    height > 0 &&
    width <= 4096 &&
    height <= 4096 &&
    width * height <= 16_000_000
  );
}
function jpegDimensions(bytes) {
  if (bytes[0] !== 255 || bytes[1] !== 216) return null;
  let offset = 2;
  const end = Math.min(bytes.length, 64 << 10);
  while (offset + 4 <= end) {
    if (bytes[offset++] !== 255) return null;
    while (offset < end && bytes[offset] === 255) offset++;
    const marker = bytes[offset++];
    if (
      marker === 0 ||
      marker === 216 ||
      marker === 217 ||
      marker === 218 ||
      marker === 1 ||
      (marker >= 208 && marker <= 215)
    )
      return null;
    const length = (bytes[offset] << 8) | bytes[offset + 1];
    if (length < 2 || offset + length > end) return null;
    if ([192, 194].includes(marker)) {
      const components = bytes[offset + 7];
      if (
        bytes[offset + 2] !== 8 ||
        ![1, 3, 4].includes(components) ||
        length !== 8 + 3 * components
      )
        return null;
      return [
        (bytes[offset + 5] << 8) | bytes[offset + 6],
        (bytes[offset + 3] << 8) | bytes[offset + 4],
      ];
    }
    offset += length;
  }
  return null;
}
let viewerSequence = 0;
export function previewKind(attachment) {
  const bytes = new Uint8Array(attachment.content);
  if (bytes.length <= 4 << 20) {
    const dimensions = jpegDimensions(bytes);
    if (dimensions && boundedImage(...dimensions)) return "jpeg";
  }
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
    if (boundedImage(width, height)) return "png";
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
    this.id = "local-attachment-" + ++viewerSequence;
    this.dialog = document.createElement("dialog");
    this.dialog.id = this.id;
    this.dialog.className =
      "m-auto max-h-[90dvh] w-[94vw] max-w-3xl overflow-y-auto rounded-2xl border border-line bg-panel p-5 text-ink backdrop:bg-black/50";
    this.dialog.setAttribute("aria-label", "Local attachment preview");
    this.close = document.createElement("button");
    this.close.type = "button";
    controlIcon(document, this.close, "x", "Close attachment preview");
    this.close.autofocus = true;
    this.close.className = "icon-action mb-3 ml-auto flex";
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
      const kind = previewKind(attachment);
      const url = URL.createObjectURL(
        new Blob([bytes], { type: "application/octet-stream" }),
      );
      this.urls.push(url);
      const row = this.document.createElement("div");
      row.className =
        "rounded-lg border border-line p-3 [overflow-wrap:anywhere]";
      const controls = this.document.createElement("div");
      controls.className = "flex items-center gap-2";
      const caption = this.document.createElement("span");
      caption.className = "min-w-0 flex-1 text-xs";
      caption.textContent = `${name} · ${bytes.length} bytes${attachment.contentId ? (kind === "png" || kind === "jpeg" ? " · Inline image" : " · Inline attachment") : ""}`;
      controls.append(caption);
      const link = this.document.createElement("a");
      link.href = url;
      link.download = name;
      controlIcon(
        this.document,
        link,
        "download",
        `Download attachment: ${name}`,
      );
      link.className = "icon-action";
      row.append(controls);
      if (kind) {
        const button = this.document.createElement("button");
        button.type = "button";
        controlIcon(
          this.document,
          button,
          "eye",
          `Preview attachment: ${name}`,
        );
        button.className = "icon-action";
        const inline = this.document.createElement("div");
        inline.hidden = true;
        inline.id = this.id + "-" + this.urls.length;
        inline.className = "mt-3 text-xs text-muted";
        button.setAttribute(
          "aria-controls",
          kind === "text" ? this.id : inline.id,
        );
        button.setAttribute(
          kind === "text" ? "aria-haspopup" : "aria-expanded",
          kind === "text" ? "dialog" : "false",
        );
        let imageURL,
          imageGeneration = 0;
        const hideImage = () => {
          imageGeneration++;
          inline.replaceChildren();
          inline.hidden = true;
          button.setAttribute("aria-expanded", "false");
          controlIcon(
            this.document,
            button,
            "eye",
            `Preview attachment: ${name}`,
          );
          if (imageURL) {
            URL.revokeObjectURL(imageURL);
            this.urls = this.urls.filter((url) => url !== imageURL);
            imageURL = null;
          }
        };
        button.addEventListener("click", () => {
          if (kind !== "text" && !inline.hidden) {
            hideImage();
            return;
          }
          this.content.replaceChildren();
          if (kind === "text") {
            const pre = this.document.createElement("pre");
            pre.className = "whitespace-pre-wrap break-words text-sm";
            pre.textContent = new TextDecoder().decode(bytes);
            this.content.append(pre);
          } else {
            const generation = ++imageGeneration;
            const image = this.document.createElement("img");
            imageURL = URL.createObjectURL(
              new Blob([bytes], { type: "image/" + kind }),
            );
            this.urls.push(imageURL);
            image.src = imageURL;
            image.alt = name;
            image.className = "max-h-[70dvh] max-w-full object-contain";
            const status = this.document.createElement("p");
            status.textContent = "Loading local image…";
            status.setAttribute("role", "status");
            image.decoding = "async";
            image.addEventListener("load", () => {
              if (generation !== imageGeneration) return;
              if (!boundedImage(image.naturalWidth, image.naturalHeight)) {
                hideImage();
                inline.hidden = false;
                status.textContent =
                  "This image exceeds preview dimensions. Download it instead.";
                inline.append(status);
              } else status.remove();
            });
            image.addEventListener("error", () => {
              if (generation !== imageGeneration || !image.isConnected) return;
              hideImage();
              inline.hidden = false;
              status.textContent =
                "This image could not be previewed. Download the original attachment instead.";
              inline.append(status);
            });
            inline.append(status, image);
            inline.hidden = false;
            button.setAttribute("aria-expanded", "true");
            controlIcon(
              this.document,
              button,
              "eye-off",
              `Hide attachment preview: ${name}`,
            );
          }
          if (kind === "text") this.dialog.showModal();
        });
        controls.append(button);
        if (kind !== "text") row.append(inline);
      } else {
        const note = this.document.createElement("p");
        note.className = "mt-1 text-xs text-muted";
        note.textContent =
          "Download available. Preview supports UTF-8 text/JSON and bounded PNG/JPEG images.";
        row.append(note);
      }
      controls.append(link);
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
