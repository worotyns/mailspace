// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import {
  messageText,
  previewDocument,
  MailHTMLPreview,
} from "../../internal/webui/assets/crypto/mail-html.mjs";
import { parseLocalMime } from "../../internal/webui/assets/crypto/mime.mjs";
const corpus = [
  `<h1>Welcome</h1><p>Safe <b>formatting</b>.</p><script>window.evil=1</script><img src="https://tracker.example.test/pixel" onerror="evil()"><a href="javascript:evil()" target="_top">Click</a>`,
  `<base href="https://tracker.example.test/"><link rel="stylesheet" href="/leak"><style>@import url(https://tracker.example.test/style);body{background:url(/leak)}</style><div style="background:url(/leak)" onclick="evil()">Content</div><form action="/leak"><input name="private"></form><iframe srcdoc="<script>evil()</script>"></iframe>`,
  `<svg><a xlink:href="javascript:evil()">SVG</a><foreignObject><iframe src="/leak"></iframe></foreignObject></svg><math><mtext><table><mglyph><style><!--</style><img title="--><img src=/leak onerror=evil()>">`,
  `<video poster="/leak"><source src="/leak"></video><audio src="/leak"></audio><object data="/leak"></object><embed src="/leak"><meta http-equiv="refresh" content="0;url=/leak"><table background="/leak"><tr><td colspan="2">Readable table</td></tr></table>`,
];
test("native MIME text is primary; HTML-only becomes inert text", async () => {
  assert.equal(
    messageText({ text: "Native text", html: "<b>Alternative</b>" }),
    "Native text",
  );
  const parsed = await parseLocalMime(
    new TextEncoder().encode(
      "Content-Type: text/html; charset=utf-8\r\n\r\n<h1>Zażółć &amp; welcome</h1><p>Readable <strong>text</strong>.</p><script>secret script</script>",
    ),
  );
  const converted = messageText(parsed);
  assert.match(converted, /Zażółć & welcome/);
  assert.match(converted, /Readable text/);
  assert.equal(converted.includes("<strong>"), false);
  assert.equal(converted.includes("secret script"), false);
  assert.throws(() => messageText({ html: "x".repeat((256 << 10) + 1) }), {
    code: "html-limit",
  });
});
test("HTML preview document excludes executable, navigating and resource-bearing markup", () => {
  const window = new JSDOM("").window;
  try {
    for (const malicious of corpus) {
      const result = previewDocument(malicious, window);
      const parsed = new JSDOM(result);
      const doc = parsed.window.document;
      assert.match(
        doc.querySelector('meta[http-equiv="Content-Security-Policy"]').content,
        /default-src 'none'/,
      );
      assert.match(
        doc.querySelector('meta[http-equiv="Content-Security-Policy"]').content,
        /connect-src 'none'/,
      );
      assert.equal(
        doc.body.querySelectorAll(
          "script,style,img,svg,math,iframe,object,embed,form,input,video,audio,source,link,meta,base,template",
        ).length,
        0,
      );
      for (const node of doc.body.querySelectorAll("*"))
        for (const attr of node.attributes) {
          assert.equal(
            /^(?:on|src|href|style|action|formaction|background|id$|name$)/i.test(
              attr.name,
            ),
            false,
            attr.name,
          );
        }
      parsed.window.close();
    }
    const doc = new JSDOM(
      previewDocument(
        "<table><tr><td>Readable table</td></tr></table>",
        window,
      ),
    );
    assert.equal(
      doc.window.document.querySelector("td").textContent,
      "Readable table",
    );
    doc.window.close();
    assert.throws(() => previewDocument("x".repeat((256 << 10) + 1), window), {
      code: "html-limit",
    });
  } finally {
    window.close();
  }
});

test("preview uses only an opaque sandboxed local document and clears it on close", () => {
  const window = new JSDOM("<dialog><div id=frame></div></dialog>").window;
  try {
    const dialog = window.document.querySelector("dialog"),
      container = window.document.getElementById("frame");
    dialog.showModal = () => {
      dialog.open = true;
    };
    dialog.close = () => {
      dialog.open = false;
    };
    const preview = new MailHTMLPreview(dialog, container, window);
    preview.open(
      "<p>PRIVATE_LOCAL_HTML_CANARY</p><script>evil()</script><img src=/leak>",
    );
    const frame = container.querySelector("iframe");
    assert.equal(frame.getAttribute("sandbox"), "");
    assert.equal(frame.src, "");
    assert.match(frame.srcdoc, /PRIVATE_LOCAL_HTML_CANARY/);
    assert.equal(frame.srcdoc.includes("<script>"), false);
    assert.equal(frame.srcdoc.includes("<img"), false);
    preview.close();
    assert.equal(container.childNodes.length, 0);
    assert.equal(dialog.open, false);
  } finally {
    window.close();
  }
});
