// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const source = await readFile(
  new URL("../crypto/label-picker.mjs", import.meta.url),
  "utf8",
);
const widget = (open = false) =>
  `<details id="labels-message-one" data-label-picker ${open ? "open" : ""}><summary>Labels +</summary><div data-label-options aria-label="Message labels"><form method="post" action="/collections/custom/membership" hx-target="#labels-message-one"><button id="choice" ${open ? "autofocus" : ""} aria-pressed="true"><span class="space-chip">Custom</span></button></form></div></details>`;
function fixture(support = true, open = false) {
  const dom = new JSDOM(
    `<article><textarea>Unsent draft</textarea><div data-inline-plaintext>Local decrypted canary</div>${widget(open)}</article>`,
    { url: "https://mail.example.test/", runScripts: "outside-only" },
  );
  if (support) {
    Object.defineProperty(dom.window.HTMLElement.prototype, "popover", {
      configurable: true,
      get() {
        return this.getAttribute("popover");
      },
    });
    const toggle = function (newState) {
      this.dataset.nativeOpen = String(newState === "open");
      this.style.display = newState === "open" ? "flex" : "none";
      const e = new dom.window.Event("toggle");
      Object.defineProperty(e, "newState", { value: newState });
      this.dispatchEvent(e);
    };
    dom.window.HTMLElement.prototype.showPopover = function () {
      toggle.call(this, "open");
    };
    dom.window.HTMLElement.prototype.hidePopover = function () {
      toggle.call(this, "closed");
    };
    const query = dom.window.document.querySelectorAll.bind(
      dom.window.document,
    );
    dom.window.document.querySelectorAll = (selector) =>
      query(
        selector === ".label-popover:popover-open"
          ? '.label-popover[data-native-open="true"]'
          : selector,
      );
  }
  dom.window.eval(source);
  return dom;
}
test("label picker progressively enhances only metadata with a native popover and compact badge controls", () => {
  const dom = fixture();
  try {
    const doc = dom.window.document,
      body = doc.querySelector("[data-inline-plaintext]"),
      draft = doc.querySelector("textarea");
    assert.equal(doc.querySelector("details"), null);
    const wrapper = doc.getElementById("labels-message-one");
    const trigger = wrapper.querySelector('button[type="button"]'),
      popup = wrapper.querySelector('[popover="auto"]');
    assert.equal(trigger.getAttribute("popovertarget"), popup.id);
    assert.equal(popup.getAttribute("role"), "dialog");
    assert.equal(
      popup.querySelector("form").getAttribute("hx-target"),
      "#labels-message-one",
    );
    popup.showPopover();
    popup.querySelector("button").focus();
    popup.hidePopover();
    assert.equal(doc.activeElement, trigger);
    assert.equal(doc.querySelector("[data-inline-plaintext]"), body);
    assert.equal(doc.querySelector("textarea"), draft);
    assert.equal(draft.value, "Unsent draft");
  } finally {
    dom.window.close();
  }
});
test("unsupported browsers keep working native details and POST actions", () => {
  const dom = fixture(false);
  try {
    assert.ok(dom.window.document.querySelector("details[data-label-picker]"));
    assert.equal(
      dom.window.document.querySelector("form").action,
      "https://mail.example.test/collections/custom/membership",
    );
    dom.window.dispatchEvent(new dom.window.Event("mailspace:lock"));
  } finally {
    dom.window.close();
  }
});
test("membership fragments reopen the popup and preserve keyboard focus without replacing body or composer", () => {
  const dom = fixture();
  try {
    const doc = dom.window.document,
      body = doc.querySelector("[data-inline-plaintext]"),
      draft = doc.querySelector("textarea");
    const old = doc.getElementById("labels-message-one");
    const replacement = doc.createElement("div");
    replacement.innerHTML = widget(true);
    old.replaceWith(replacement.firstElementChild);
    doc.dispatchEvent(
      new dom.window.CustomEvent("htmx:afterSwap", {
        detail: { target: doc.getElementById("labels-message-one") },
      }),
    );
    assert.equal(
      doc.querySelector(".label-popover").dataset.nativeOpen,
      "true",
    );
    assert.equal(doc.activeElement.id, "choice");
    assert.equal(doc.querySelector("[data-inline-plaintext]"), body);
    assert.equal(doc.querySelector("textarea"), draft);
  } finally {
    dom.window.close();
  }
});

test("focus-induced scroll does not immediately dismiss the popup; scrolling outside it does", () => {
  const dom = fixture();
  try {
    const doc = dom.window.document,
      popup = doc.querySelector(".label-popover");
    popup.showPopover();
    doc.querySelector("article").dispatchEvent(new dom.window.Event("scroll"));
    assert.equal(popup.dataset.nativeOpen, "true");
    popup.dispatchEvent(new dom.window.Event("wheel", { bubbles: true }));
    assert.equal(popup.dataset.nativeOpen, "true");
    doc
      .querySelector("article")
      .dispatchEvent(new dom.window.Event("wheel", { bubbles: true }));
    assert.equal(popup.dataset.nativeOpen, "false");
  } finally {
    dom.window.close();
  }
});
