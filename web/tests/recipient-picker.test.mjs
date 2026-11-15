// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const source = await readFile(
  new URL("../crypto/recipient-picker.mjs", import.meta.url),
  "utf8",
);
function fixture() {
  const dom = new JSDOM(
    `<form><input id="to" data-recipient-picker list="to-list"><datalist id="to-list"></datalist><input id="cc" data-recipient-picker list="cc-list"><datalist id="cc-list"></datalist><main id="timeline-shell"></main></form>`,
    { url: "https://mail.example.test/", runScripts: "outside-only" },
  );
  const timers = new Map();
  const pending = [];
  const calls = [];
  let id = 0;
  dom.window.setTimeout = (fn) => {
    const timer = ++id;
    timers.set(timer, fn);
    return timer;
  };
  dom.window.clearTimeout = (timer) => {
    timers.delete(timer);
  };
  dom.window.fetch = (url) => {
    calls.push(String(url));
    return new Promise((resolve) =>
      pending.push({ url: String(url), resolve }),
    );
  };
  dom.window.eval(
    source.replace(/\bexport\s+/g, "") +
      "\nwindow.__picker = { recipientFragment };",
  );
  const runTimers = () => {
    for (const [timer, fn] of [...timers]) {
      timers.delete(timer);
      fn();
    }
  };
  const settle = () => new Promise((resolve) => setImmediate(resolve));
  return { dom, calls, pending, runTimers, settle };
}
const response = (items) => ({ ok: true, json: async () => items });
function type(dom, id, value) {
  const input = dom.window.document.getElementById(id);
  input.value = value;
  input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
}
test("recipient fragment keeps earlier recipients and only queries the typed tail", async () => {
  const { dom } = fixture();
  try {
    const { recipientFragment } = dom.window.__picker;
    const fragment = (value) => ({
      ...recipientFragment(value),
    });
    assert.deepEqual(fragment("alice@example.test, jo"), {
      prefix: "alice@example.test,",
      query: "jo",
    });
    assert.deepEqual(fragment('"Last, First" <a@example.test>; an'), {
      prefix: '"Last, First" <a@example.test>;',
      query: "an",
    });
    assert.deepEqual(fragment("first@example.test;joe"), {
      prefix: "first@example.test;",
      query: "joe",
    });
    assert.deepEqual(fragment("sole@example.test"), {
      prefix: "",
      query: "sole@example.test",
    });
  } finally {
    dom.window.close();
  }
});
test("stale or aborted suggestion responses never overwrite newer input", async () => {
  const { dom, calls, pending, runTimers, settle } = fixture();
  try {
    type(dom, "to", "alice");
    runTimers();
    assert.equal(pending.length, 1);
    assert.equal(calls[0], "/people/suggestions?q=alice");
    // A newer keystroke cancels the pending debounce for the same input.
    type(dom, "to", "bob");
    runTimers();
    assert.equal(pending.length, 2);
    assert.equal(calls[1], "/people/suggestions?q=bob");
    pending[1].resolve(response([{ email: "bob@example.test", name: "Bob" }]));
    await settle();
    const list = dom.window.document.getElementById("to-list");
    assert.equal(list.children.length, 1);
    assert.equal(list.children[0].value, "bob@example.test");
    // The older response arrives late; it must not append or replace results.
    pending[0].resolve(
      response([{ email: "alice@example.test", name: "Alice" }]),
    );
    await settle();
    assert.equal(list.children.length, 1);
    assert.equal(list.children[0].value, "bob@example.test");
    // A response for a value the user has since changed is also ignored.
    type(dom, "to", "carol");
    runTimers();
    const carol = pending.at(-1);
    const input = dom.window.document.getElementById("to");
    input.value = "carol@example.test";
    carol.resolve(response([{ email: "carol@example.test", name: "Carol" }]));
    await settle();
    assert.equal(list.children.length, 0);
  } finally {
    dom.window.close();
  }
});
test("suggestions keep the typed recipient prefix and stay free-form on failure", async () => {
  const { dom, pending, runTimers, settle } = fixture();
  try {
    type(dom, "cc", "alice@example.test, jo");
    runTimers();
    assert.equal(pending.length, 1);
    assert.equal(pending[0].url, "/people/suggestions?q=jo");
    pending[0].resolve(
      response([{ email: "joe@example.test", name: "Joe <script>" }]),
    );
    await settle();
    const list = dom.window.document.getElementById("cc-list");
    assert.equal(list.children.length, 1);
    assert.equal(
      list.children[0].value,
      "alice@example.test, joe@example.test",
    );
    assert.equal(list.children[0].label, "Joe <script>");
    // Clearing the timeline shell drops mounted suggestions without touching input.
    dom.window.document.getElementById("timeline-shell").dispatchEvent(
      new dom.window.CustomEvent("htmx:beforeSwap", {
        bubbles: true,
        detail: {
          target: dom.window.document.getElementById("timeline-shell"),
        },
      }),
    );
    assert.equal(list.children.length, 0);
    assert.equal(
      dom.window.document.getElementById("cc").value,
      "alice@example.test, jo",
    );
  } finally {
    dom.window.close();
  }
});
