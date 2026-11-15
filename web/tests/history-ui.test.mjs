// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const source = (
  await readFile(new URL("../crypto/history-ui.mjs", import.meta.url), "utf8")
).replace(/^import .*;$/gm, "");
const control = `<nav id="timeline-history" data-history-controls><a data-history-load href="/?before=opaque">Older</a><p data-history-status></p></nav>`;
function fixture() {
  const dom = new JSDOM(
    `<section id="timeline-feed">${control}<div data-history-row data-history-key="retained"><article data-message-key="retained"><div data-inline-plaintext>PRIVATE_DECRYPTED_CANARY</div></article></div></section><form><textarea>Unsent authored draft</textarea></form>`,
    { url: "https://mail.example.test/", runScripts: "outside-only" },
  );
  const doc = dom.window.document,
    feed = doc.getElementById("timeline-feed"),
    anchor = feed.querySelector("article");
  feed.getBoundingClientRect = () => ({ top: 0 });
  anchor.getBoundingClientRect = () => ({ top: 50, bottom: 100 });
  let enhancements = 0,
    aborted = 0,
    clicks = 0;
  new dom.window.Function("enhanceReadableMessages", source)(
    () => enhancements++,
  );
  doc.querySelector("[data-history-load]").addEventListener("click", (e) => {
    clicks++;
    e.preventDefault();
  });
  const event = (name, extra = {}) => {
    const e = new dom.window.CustomEvent(name, {
      bubbles: true,
      cancelable: true,
      detail: {
        elt: doc.querySelector("[data-history-load]"),
        target: doc.getElementById("timeline-history"),
        xhr: {
          abort() {
            aborted++;
          },
        },
        ...extra,
      },
    });
    doc.dispatchEvent(e);
    return e;
  };
  return {
    dom,
    doc,
    feed,
    anchor,
    event,
    get enhancements() {
      return enhancements;
    },
    get aborted() {
      return aborted;
    },
    get clicks() {
      return clicks;
    },
  };
}
test("upward prepend retains decrypted/body/composer DOM and corrects the same visible row's position", () => {
  const f = fixture();
  try {
    const privateNode = f.doc.querySelector("[data-inline-plaintext]"),
      draft = f.doc.querySelector("textarea"),
      old = f.doc.querySelector("[data-history-row]");
    f.feed.scrollTop = 100;
    f.doc.querySelector("[data-history-load]").focus();
    f.event("htmx:beforeRequest");
    assert.equal(f.feed.hasAttribute("aria-busy"), false);
    assert.equal(f.event("htmx:beforeRequest").defaultPrevented, true);
    const newNodes = f.doc.createElement("div");
    newNodes.innerHTML =
      control +
      `<div data-history-row data-history-key="older"><article data-message-key="older"></article></div><div data-history-row data-history-key="retained"><article data-message-key="retained"></article></div>`;
    f.doc
      .getElementById("timeline-history")
      .replaceWith(...newNodes.childNodes);
    f.anchor.getBoundingClientRect = () => ({ top: 250, bottom: 300 });
    f.event("htmx:afterSwap");
    assert.equal(f.feed.scrollTop, 300);
    assert.equal(f.enhancements, 1);
    assert.equal(
      f.doc.activeElement,
      f.doc.querySelector("[data-history-load]"),
    );
    assert.equal(
      f.doc.querySelectorAll('[data-history-key="retained"]').length,
      1,
    );
    assert.equal(f.doc.querySelector('[data-history-key="retained"]'), old);
    assert.equal(f.doc.querySelector("[data-inline-plaintext]"), privateNode);
    assert.equal(f.doc.querySelector("textarea"), draft);
    assert.equal(draft.value, "Unsent authored draft");
    assert.equal(f.dom.window.localStorage.length, 0);
  } finally {
    f.dom.window.close();
  }
});
test("only an upward movement near the top requests history, and errors wait for an explicit retry", () => {
  const f = fixture();
  try {
    const scroll = (top) => {
      f.feed.scrollTop = top;
      f.feed.dispatchEvent(new f.dom.window.Event("scroll"));
    };
    scroll(500);
    scroll(300);
    assert.equal(f.clicks, 0);
    scroll(100);
    assert.equal(f.clicks, 1);
    f.event("htmx:beforeRequest");
    scroll(50);
    assert.equal(f.clicks, 1);
    f.event("htmx:afterRequest", { successful: false });
    assert.match(
      f.doc.querySelector("[data-history-status]").textContent,
      /Retry/,
    );
    scroll(10);
    assert.equal(f.clicks, 1);
    f.event("htmx:beforeRequest");
    assert.equal(
      f.doc.querySelector("[data-history-controls]").dataset.historyFailed,
      undefined,
    );
  } finally {
    f.dom.window.close();
  }
});
test("root replacement and pagehide abort pending GET without serializing private fields or leaving a busy control", () => {
  for (const type of ["replace", "pagehide"]) {
    const f = fixture();
    try {
      f.event("htmx:beforeRequest");
      if (type === "replace")
        f.event("htmx:beforeSwap", { target: { id: "timeline-shell" } });
      else f.dom.window.dispatchEvent(new f.dom.window.Event("pagehide"));
      assert.equal(f.aborted, 1);
      assert.equal(
        f.doc
          .querySelector("[data-history-controls]")
          .hasAttribute("aria-busy"),
        false,
      );
      assert.equal(
        f.doc.querySelector("textarea").value,
        "Unsent authored draft",
      );
    } finally {
      f.dom.window.close();
    }
  }
});
