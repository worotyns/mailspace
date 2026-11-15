// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const source = (
  await readFile(new URL("../crypto/timeline-ui.mjs", import.meta.url), "utf8")
).replace(/^import .*;$/gm, "");
for (const type of ["mailspace:panel-closed", "mailspace:sent"]) {
  test(`${type} refresh selects only timeline, not a nested application document`, () => {
    const dom = new JSDOM(
      '<section id="timeline-shell" data-feed-url="/?space=synthetic"></section>',
      { url: "https://mail.example.test/", runScripts: "outside-only" },
    );
    try {
      const calls = [];
      dom.window.htmx = { ajax: (...args) => calls.push(args) };
      new dom.window.Function("enhanceReadableMessages", source)(() => {});
      dom.window.dispatchEvent(
        new dom.window.CustomEvent(type, {
          detail: { state: "accepted", location: "/outbox" },
        }),
      );
      assert.equal(calls.length, 1);
      assert.equal(calls[0][2].target, "#timeline-shell");
      assert.equal(calls[0][2].select, "#timeline-shell");
      assert.equal(calls[0][2].swap, "outerHTML");
    } finally {
      dom.window.close();
    }
  });
}

test("inline recorded send refreshes once and clears the submitted draft, while validation errors keep it", () => {
  const dom = new JSDOM(
    '<section id="timeline-shell" data-feed-url="/?space=synthetic&reply=original"><form><textarea>draft</textarea><button>Send</button><div data-reply-status></div></form></section>',
    { url: "https://mail.example.test/", runScripts: "outside-only" },
  );
  try {
    const calls = [];
    dom.window.htmx = { ajax: (...args) => calls.push(args) };
    new dom.window.Function("enhanceReadableMessages", source)(() => {});
    const status = dom.window.document.querySelector("[data-reply-status]");
    const settle = () =>
      dom.window.document.dispatchEvent(
        new dom.window.CustomEvent("htmx:afterSettle", {
          detail: { target: status },
        }),
      );
    status.textContent = "Recipient invalid";
    settle();
    assert.equal(calls.length, 0);
    assert.equal(dom.window.document.querySelector("textarea").value, "draft");
    status.innerHTML =
      '<div data-outgoing="synthetic" data-send-state="accepted" data-outbox-url="/outbox?message=synthetic"></div>';
    settle();
    settle(); // Repeated settle notifications must not trigger another refresh.
    assert.equal(calls.length, 1);
    assert.equal(calls[0][1], "/?space=synthetic");
    assert.equal(calls[0][2].select, "#timeline-shell");
    assert.equal(dom.window.document.querySelector("textarea").value, "");
    assert.equal(dom.window.document.querySelector("button").disabled, true);
  } finally {
    dom.window.close();
  }
});

test("sending from a thread refreshes its scoped fragment and retains original thread context", () => {
  const dom = new JSDOM(
    '<section id="timeline-shell" data-feed-url="/?space=synthetic"><dialog id="thread-drawer" open><div id="thread-content"><div data-thread-url="/?space=synthetic&thread=original&reply=original"></div></div></dialog></section>',
    { url: "https://mail.example.test/", runScripts: "outside-only" },
  );
  try {
    const calls = [];
    dom.window.htmx = { ajax: (...args) => calls.push(args) };
    new dom.window.Function("enhanceReadableMessages", source)(() => {});
    dom.window.dispatchEvent(
      new dom.window.CustomEvent("mailspace:sent", {
        detail: { state: "unknown", location: "/outbox" },
      }),
    );
    assert.equal(calls.length, 1);
    assert.equal(calls[0][1], "/?space=synthetic&thread=original");
    assert.equal(calls[0][2].target, "#thread-content");
    assert.equal(calls[0][2].swap, "innerHTML");
    const drawer = dom.window.document.getElementById("thread-drawer");
    drawer.removeAttribute("open");
    drawer.dispatchEvent(new dom.window.Event("close"));
    assert.equal(calls.length, 2);
    assert.equal(calls[1][2].target, "#timeline-shell");
    assert.equal(calls[1][2].select, "#timeline-shell");
  } finally {
    dom.window.close();
  }
});
