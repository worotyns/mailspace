// SPDX-License-Identifier: AGPL-3.0-only
import { previewCSS } from "./preview-css.mjs";
import createDOMPurify from "../vendor/dompurify/purify.es.mjs";
import { htmlToText } from "../vendor/postal-mime/text-format.js";
import { VaultError } from "./errors.mjs";
const maxHTML = 256 << 10;
export function messageText(parsed) {
  if (typeof parsed.text === "string" && parsed.text.trim()) return parsed.text;
  if (typeof parsed.html === "string") {
    if (new TextEncoder().encode(parsed.html).length > maxHTML)
      throw new VaultError("html-limit");
    return htmlToText(parsed.html);
  }
  return "No readable text body.";
}
// The only HTML rendering boundary: a sanitized document in an opaque sandboxed
// srcdoc frame. Mail HTML is never inserted into the application's DOM.
export function previewDocument(source, window) {
  if (
    typeof source !== "string" ||
    new TextEncoder().encode(source).length > maxHTML
  )
    throw new VaultError("html-limit");
  const purifier = createDOMPurify(window);
  if (!purifier.isSupported) throw new VaultError("html-unavailable");
  const safe = purifier.sanitize(source, {
    ALLOWED_TAGS: [
      "p",
      "br",
      "div",
      "span",
      "b",
      "strong",
      "i",
      "em",
      "u",
      "s",
      "h1",
      "h2",
      "h3",
      "h4",
      "h5",
      "h6",
      "blockquote",
      "pre",
      "code",
      "ul",
      "ol",
      "li",
      "table",
      "thead",
      "tbody",
      "tfoot",
      "tr",
      "td",
      "th",
      "caption",
      "hr",
      "a",
    ],
    ALLOWED_ATTR: ["colspan", "rowspan", "title", "dir", "lang"],
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
    FORBID_TAGS: [
      "script",
      "style",
      "form",
      "iframe",
      "object",
      "embed",
      "svg",
      "math",
      "template",
      "img",
      "video",
      "audio",
      "source",
      "link",
      "meta",
      "base",
    ],
    FORBID_ATTR: [
      "style",
      "href",
      "src",
      "srcset",
      "action",
      "formaction",
      "background",
      "id",
      "name",
    ],
  });
  const csp =
    "default-src 'none'; script-src 'none'; connect-src 'none'; img-src 'none'; font-src 'none'; media-src 'none'; frame-src 'none'; object-src 'none'; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'";
  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${csp}"><meta name="viewport" content="width=device-width,initial-scale=1"><style>${previewCSS}</style></head><body>${safe}</body></html>`;
}
export class MailHTMLPreview {
  #dialog;
  #container;
  #window;
  constructor(dialog, container, window) {
    this.#dialog = dialog;
    this.#container = container;
    this.#window = window;
    dialog.addEventListener("close", () => this.clear());
  }
  open(source) {
    if (typeof this.#dialog.showModal !== "function")
      throw new VaultError("html-unavailable");
    const doc = previewDocument(source, this.#window);
    this.clear();
    const frame = this.#window.document.createElement("iframe");
    frame.setAttribute("sandbox", "");
    frame.referrerPolicy = "no-referrer";
    frame.title = "Isolated email HTML preview";
    frame.className = "h-[65dvh] w-full border-0 bg-white";
    frame.srcdoc = doc;
    this.#container.append(frame);
    this.#dialog.showModal();
  }
  clear() {
    this.#container.replaceChildren();
  }
  close() {
    this.#dialog.close?.();
    this.clear();
  }
}
