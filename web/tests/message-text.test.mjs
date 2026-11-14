// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import {
  renderReadableText,
  enhanceReadableMessages,
} from "../../internal/webui/assets/crypto/message-text.mjs";
function fixture() {
  const dom = new JSDOM("<main></main>");
  return dom.window.document.querySelector("main");
}
test("readable text formats Markdown and compacts blank lines without changing literal code", () => {
  const root = fixture();
  renderReadableText(
    root,
    "# Update\n\n\n\n**Ready** and *review*\n\n- first\n- second\n\n```txt\n> literal\n\n\ncode\n```",
  );
  assert.equal(root.querySelector("h3").textContent, "Update");
  assert.equal(root.querySelector("strong").textContent, "Ready");
  assert.equal(root.querySelectorAll("li").length, 2);
  assert.equal(
    root.querySelector("pre code").textContent,
    "> literal\n\n\ncode",
  );
  assert.equal(root.querySelectorAll("details").length, 0);
  assert.ok(root.hasAttribute("hx-disable"));
});
test("quotes fold while surrounding replies remain visible", () => {
  const root = fixture();
  renderReadableText(
    root,
    "New answer\n\n> old answer\n> > older answer\n\nAnother answer",
  );
  assert.equal(root.querySelectorAll("details").length, 1);
  assert.match(root.querySelector("details").textContent, /old answer/);
  assert.match(root.lastElementChild.textContent, /Another answer/);
});
test("untrusted Markdown and HTML cannot create resources, navigation or controls", () => {
  const root = fixture();
  renderReadableText(
    root,
    '<script>PRIVATE_CANARY</script>\n\n<img src="https://tracker.example.test/pixel">\n\n[Open](javascript:alert(1)) ![remote](https://tracker.example.test/a.png)\n\n<form action="/leak"><input name="secret"></form>\n\n- [x] done',
  );
  assert.equal(
    root.querySelectorAll(
      "script,img,a,form,input,iframe,style,[src],[href],[action],[onclick]",
    ).length,
    0,
  );
  assert.match(root.textContent, /PRIVATE_CANARY/);
  assert.match(root.textContent, /☑/);
  assert.equal(root.querySelectorAll("[data-plaintext]").length, 0);
});
test("display has bounded output and safe fallback on excessive nesting", () => {
  const root = fixture();
  renderReadableText(root, `${"> ".repeat(100)}deep`);
  assert.match(root.textContent, /deep/);
  assert.ok(root.querySelectorAll("*").length < 2100);
  renderReadableText(root, "x".repeat(150000));
  assert.match(root.textContent, /Inline text shortened/);
  assert.ok(root.textContent.length < 132000);
});
test("ordinary message enhancement reconstructs only original text parts and runs once", () => {
  const root = fixture();
  root.innerHTML =
    "<section data-readable-message><p data-readable-part>**Ready**\n\n</p><details><summary>…</summary><p data-readable-part>&gt; old</p></details></section>";
  enhanceReadableMessages(root);
  const message = root.firstElementChild;
  assert.equal(message.querySelector("strong").textContent, "Ready");
  assert.equal(message.querySelectorAll("summary").length, 1);
  const first = message.firstChild;
  enhanceReadableMessages(root);
  assert.equal(message.firstChild, first);
});
