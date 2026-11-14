// SPDX-License-Identifier: AGPL-3.0-only
// Markdown tokens become fixed DOM elements, never HTML, resources or actions.
import { Lexer } from "../vendor/marked/marked.esm.js";
const LIMIT = 128 << 10;
const decodeText = (value) =>
  String(value || "").replace(
    /&(amp|lt|gt|quot|#39);/g,
    (_, entity) =>
      ({ amp: "&", lt: "<", gt: ">", quot: '"', "#39": "'" })[entity],
  );
// Email replies do not use Markdown's lazy blockquote continuation: an
// unprefixed reply must never disappear inside the preceding folded quote.
function explicitQuotes(source) {
  let fence = null,
    previousQuote = false;
  const lines = [];
  for (const line of source.split("\n")) {
    const marker = /^ {0,3}(`{3,}|~{3,})/.exec(line);
    const quoted = !fence && /^ {0,3}>/.test(line);
    if (!fence && previousQuote !== quoted && line.trim()) lines.push("");
    lines.push(line);
    if (marker) {
      if (!fence) fence = marker[1];
      else if (
        marker[1][0] === fence[0] &&
        marker[1].length >= fence.length &&
        /^ {0,3}(`+|~+)\s*$/.test(line)
      )
        fence = null;
    }
    previousQuote = quoted;
  }
  return lines.join("\n");
}
export function renderReadableText(
  container,
  value,
  document = container.ownerDocument,
) {
  const original = String(value || ""),
    source = original.slice(0, LIMIT).replace(/\r\n?/g, "\n");
  let remaining = 2048;
  const text = (parent, content) =>
    parent.append(document.createTextNode(decodeText(content)));
  const element = (parent, tag, classes = "") => {
    if (--remaining < 0) throw new Error("display limit");
    const child = document.createElement(tag);
    if (classes) child.className = classes;
    parent.append(child);
    return child;
  };
  function render(parent, tokens, depth = 0, quoted = false) {
    if (depth > 12) throw new Error("display depth");
    for (const token of tokens || []) {
      if (--remaining < 0) throw new Error("display limit");
      switch (token.type) {
        case "space":
          break;
        case "paragraph":
          render(
            element(parent, "p", "whitespace-pre-wrap wrap-anywhere"),
            token.tokens,
            depth + 1,
            quoted,
          );
          break;
        case "text":
          if (token.tokens) render(parent, token.tokens, depth + 1, quoted);
          else text(parent, token.text);
          break;
        case "escape":
          text(parent, token.text);
          break;
        case "strong":
        case "em":
        case "del":
          render(element(parent, token.type), token.tokens, depth + 1, quoted);
          break;
        case "codespan":
          text(
            element(
              parent,
              "code",
              "rounded bg-hover px-1 font-mono text-[0.95em]",
            ),
            token.text,
          );
          break;
        case "code": {
          const pre = element(
            parent,
            "pre",
            "max-w-full overflow-x-auto rounded-lg border border-line bg-canvas p-3 text-xs",
          );
          // Code is literal; entity/blank-line normalization would change it.
          element(pre, "code").textContent = token.text;
          break;
        }
        case "br":
          element(parent, "br");
          break;
        case "heading":
          render(
            element(
              parent,
              `h${Math.max(3, Math.min(6, Number(token.depth) + 2))}`,
              "font-semibold",
            ),
            token.tokens,
            depth + 1,
            quoted,
          );
          break;
        case "hr":
          element(parent, "hr", "border-line");
          break;
        case "blockquote": {
          const wrapper = quoted
            ? element(
                parent,
                "blockquote",
                "border-l border-line pl-3 text-muted",
              )
            : element(parent, "details", "message-quote");
          if (!quoted) {
            const summary = element(wrapper, "summary");
            summary.setAttribute("aria-label", "Show or hide quoted text");
            summary.textContent = "…";
          }
          render(
            element(wrapper, "div", "space-y-2"),
            token.tokens,
            depth + 1,
            true,
          );
          break;
        }
        case "list": {
          const list = element(
            parent,
            token.ordered ? "ol" : "ul",
            `space-y-1 pl-5 ${token.ordered ? "list-decimal" : "list-disc"}`,
          );
          if (
            token.ordered &&
            Number.isInteger(token.start) &&
            token.start > 0 &&
            token.start <= 10000
          )
            list.start = token.start;
          for (const item of token.items || []) {
            const li = element(list, "li");
            if (item.task) text(li, item.checked ? "☑ " : "☐ ");
            render(li, item.tokens, depth + 1, quoted);
          }
          break;
        }
        case "link": {
          const span = element(parent, "span");
          render(span, token.tokens, depth + 1, quoted);
          const target = decodeText(token.href),
            label = span.textContent;
          // Keep URLs as selectable text, including decrypted URLs. No navigation
          // attributes, automatic fetch, tracking image or generic export sink.
          if (target && target !== label && target !== `mailto:${label}`)
            text(span, ` (${target})`);
          break;
        }
        case "image":
          text(parent, token.text ? `[Image: ${token.text}]` : "[Image]");
          break;
        case "table": {
          if (token.rows.length > 100 || token.header.length > 20)
            throw new Error("display table limit");
          const table = element(
            element(parent, "div", "max-w-full overflow-x-auto"),
            "table",
            "w-full border-collapse text-left text-xs",
          );
          const header = element(element(table, "thead"), "tr");
          for (const cell of token.header) {
            const th = element(
              header,
              "th",
              "border border-line p-2 font-semibold",
            );
            th.scope = "col";
            render(th, cell.tokens, depth + 1, quoted);
          }
          const body = element(table, "tbody");
          for (const row of token.rows) {
            const tr = element(body, "tr");
            for (const cell of row)
              render(
                element(tr, "td", "border border-line p-2"),
                cell.tokens,
                depth + 1,
                quoted,
              );
          }
          break;
        }
        default:
          text(parent, token.raw || token.text || "");
      }
    }
  }
  const fragment = document.createDocumentFragment();
  try {
    render(
      fragment,
      Lexer.lex(explicitQuotes(source), { gfm: true, breaks: true }),
    );
  } catch {
    fragment.replaceChildren();
    const p = document.createElement("p");
    p.className = "whitespace-pre-wrap wrap-anywhere";
    p.textContent = source.replace(/\n(?:[ \t]*\n){2,}/g, "\n\n");
    fragment.append(p);
  }
  if (original.length > LIMIT) {
    const note = document.createElement("p");
    note.className = "text-xs text-muted";
    note.textContent =
      "Inline text shortened. Open the full message to read the rest.";
    fragment.append(note);
  }
  container.classList.add("space-y-2");
  container.setAttribute("hx-disable", "");
  container.replaceChildren(fragment);
}
export function enhanceReadableMessages(scope) {
  for (const container of scope.querySelectorAll(
    "[data-readable-message]:not([data-readable-ready])",
  )) {
    const parts = [...container.querySelectorAll("[data-readable-part]")];
    if (!parts.length) continue;
    const source = parts.map((part) => part.textContent).join("");
    renderReadableText(container, source);
    container.dataset.readableReady = "true";
  }
}
