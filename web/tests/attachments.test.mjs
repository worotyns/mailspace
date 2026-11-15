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

test("CID images reveal only bounded local PNG/JPEG inline and stale callbacks cannot restore hidden content", () => {
  const dom = new JSDOM('<div id="attachments"></div>');
  const viewer = new AttachmentViewer(
    dom.window.document.getElementById("attachments"),
    dom.window.document,
  );
  const png = new Uint8Array(33);
  png.set([137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82]);
  new DataView(png.buffer).setUint32(16, 2);
  new DataView(png.buffer).setUint32(20, 2);
  // A structural 8-bit three-component JPEG SOF fixture, not a real photograph.
  const jpeg = new Uint8Array([
    255, 216, 255, 192, 0, 17, 8, 0, 2, 0, 2, 3, 1, 17, 0, 2, 17, 0, 3, 17, 0,
    255, 217,
  ]);
  assert.equal(previewKind({ content: jpeg }), "jpeg");
  const oversized = jpeg.slice();
  oversized[7] = 255;
  oversized[8] = 255;
  assert.equal(previewKind({ content: oversized }), null);
  const malformed = jpeg.slice();
  malformed[5] = 1;
  assert.equal(previewKind({ content: malformed }), null);
  try {
    viewer.render([
      { filename: "cid.png", contentId: "local@example.test", content: png },
    ]);
    const doc = dom.window.document;
    assert.match(doc.getElementById("attachments").textContent, /Inline image/);
    assert.equal(doc.querySelectorAll("img").length, 0);
    assert.ok(
      doc.querySelector(
        'a[download][aria-label="Download attachment: cid.png"]',
      ),
    );
    const eye = doc.querySelector(
      '#attachments button[aria-label="Preview attachment: cid.png"]',
    );
    eye.click();
    const old = doc.querySelector("img");
    assert.ok(old.src.startsWith("blob:"));
    assert.equal(viewer.dialog.open, false);
    assert.equal(eye.getAttribute("aria-expanded"), "true");
    assert.equal(viewer.urls.length, 2);
    eye.click();
    assert.equal(doc.querySelectorAll("img").length, 0);
    assert.equal(viewer.urls.length, 1);
    eye.click();
    const current = doc.querySelector("img");
    old.dispatchEvent(new dom.window.Event("error"));
    assert.equal(doc.querySelector("img"), current);
    viewer.clear();
    current.dispatchEvent(new dom.window.Event("error"));
    assert.equal(doc.querySelectorAll("img").length, 0);
    assert.equal(viewer.urls.length, 0);
    assert.equal(doc.getElementById("attachments").textContent, "");
  } finally {
    viewer.dispose();
    dom.window.close();
  }
});
