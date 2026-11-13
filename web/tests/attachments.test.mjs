// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import {
  AttachmentViewer,
  previewKind,
} from "../../internal/webui/assets/crypto/attachments.mjs";
test("local attachment previews are inert, bounded and removed on lock/clear", () => {
  const dom = new JSDOM('<div id="attachments"></div>');
  const doc = dom.window.document;
  dom.window.HTMLDialogElement.prototype.showModal = function () {
    this.open = true;
  };
  dom.window.HTMLDialogElement.prototype.close = function () {
    this.open = false;
    this.dispatchEvent(new dom.window.Event("close"));
  };
  const viewer = new AttachmentViewer(doc.getElementById("attachments"), doc);
  const text = {
    filename: "<script>hostile.txt",
    mimeType: "text/plain",
    content: new TextEncoder().encode(
      '<img src="https://tracker.example.test/">PRIVATE_ATTACHMENT',
    ),
  };
  assert.equal(previewKind(text), "text");
  for (const mimeType of ["text/html", "image/svg+xml", "application/pdf"])
    assert.equal(previewKind({ ...text, mimeType }), null);
  const png = new Uint8Array(33);
  png.set([137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82]);
  new DataView(png.buffer).setUint32(16, 100000);
  new DataView(png.buffer).setUint32(20, 100000);
  assert.equal(previewKind({ content: png }), null);
  new DataView(png.buffer).setUint32(16, 1);
  new DataView(png.buffer).setUint32(20, 1);
  assert.equal(previewKind({ content: png }), "png");
  png[12] = 0;
  assert.equal(previewKind({ content: png }), null);
  try {
    viewer.render([text]);
    assert.equal(doc.querySelectorAll("a[download]").length, 1);
    doc.querySelector("#attachments button").click();
    assert.equal(doc.querySelectorAll("img,script").length, 0);
    assert.match(doc.querySelector("pre").textContent, /PRIVATE_ATTACHMENT/);
    viewer.clear();
    assert.equal(doc.querySelectorAll("pre,a[download]").length, 0);
    assert.equal(viewer.dialog.open, false);
  } finally {
    viewer.dispose();
    dom.window.close();
  }
});
