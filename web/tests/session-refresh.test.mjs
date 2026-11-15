// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const source = await readFile(
  new URL("../../internal/webui/assets/session.js", import.meta.url),
  "utf8",
);
const csrf = "N".repeat(43),
  expiry = new Date(Date.now() + 600000).toISOString();
function fixture(fetcher) {
  const dom = new JSDOM(
    `<div id="timeline-shell" data-user-id="owner" data-csrf="old" data-session-expires="1000"><form method="post" action="/timeline/replies/plain"><input name="csrf" value="old"><input name="to" value="recipient@example.test"><textarea name="body">Unsent authored draft</textarea><button>Send</button></form></div>`,
    { url: "https://mail.example.test/", runScripts: "outside-only" },
  );
  dom.window.fetch = fetcher;
  dom.window.BroadcastChannel = class {
    addEventListener() {}
    postMessage() {}
  };
  dom.window.eval(source);
  return dom;
}
const settle = async () => {
  for (let i = 0; i < 8; i++) await Promise.resolve();
};
test("session rotation refreshes only authenticated metadata, preserving the current timeline and unsent controls", async () => {
  const calls = [];
  let complete;
  const dom = fixture(async (url, options) => {
    calls.push({ url, options });
    return new Promise((resolve) => {
      complete = resolve;
    });
  });
  try {
    const { document, Event, CustomEvent } = dom.window;
    const shell = document.getElementById("timeline-shell"),
      form = shell.querySelector("form");
    dom.window.dispatchEvent(new Event("mailspace:session-rotated"));
    const submit = new Event("submit", { bubbles: true, cancelable: true });
    form.dispatchEvent(submit);
    assert.equal(submit.defaultPrevented, true);
    const post = new CustomEvent("htmx:configRequest", {
      bubbles: true,
      cancelable: true,
      detail: { verb: "post" },
    });
    form.dispatchEvent(post);
    assert.equal(post.defaultPrevented, true);
    complete({
      ok: true,
      json: async () => ({
        user: { id: "owner" },
        csrfToken: csrf,
        expiresAt: expiry,
      }),
    });
    await settle();
    assert.equal(document.getElementById("timeline-shell"), shell);
    assert.equal(shell.dataset.csrf, csrf);
    assert.equal(form.elements.csrf.value, csrf);
    assert.equal(shell.dataset.sessionExpires, String(Date.parse(expiry)));
    assert.equal(form.elements.body.value, "Unsent authored draft");
    assert.equal(form.elements.to.value, "recipient@example.test");
    const ready = new Event("submit", { bubbles: true, cancelable: true });
    form.dispatchEvent(ready);
    assert.equal(ready.defaultPrevented, false);
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, "/api/session");
    assert.equal(calls[0].options.credentials, "same-origin");
    assert.equal(calls[0].options.cache, "no-store");
    assert.equal(calls[0].options.body, undefined);
  } finally {
    dom.window.close();
  }
});
test("stale responses and foreign or invalid session metadata cannot replace current CSRF", async () => {
  const pending = [];
  const dom = fixture(() => new Promise((resolve) => pending.push(resolve)));
  try {
    const { document, Event } = dom.window;
    const form = document.querySelector("form");
    dom.window.dispatchEvent(new Event("mailspace:session-rotated"));
    dom.window.dispatchEvent(new Event("mailspace:session-rotated"));
    pending[1]({
      ok: true,
      json: async () => ({
        user: { id: "owner" },
        csrfToken: csrf,
        expiresAt: expiry,
      }),
    });
    await settle();
    pending[0]({
      ok: true,
      json: async () => ({
        user: { id: "foreign" },
        csrfToken: "F".repeat(43),
        expiresAt: expiry,
      }),
    });
    await settle();
    assert.equal(form.elements.csrf.value, csrf);
    for (const data of [
      { user: { id: "foreign" }, csrfToken: "F".repeat(43), expiresAt: expiry },
      { user: { id: "owner" }, csrfToken: "", expiresAt: expiry },
      { user: { id: "owner" }, csrfToken: csrf, expiresAt: "invalid" },
    ]) {
      dom.window.dispatchEvent(new Event("mailspace:session-rotated"));
      pending.at(-1)({ ok: true, json: async () => data });
      await settle();
      assert.equal(form.elements.csrf.value, csrf);
      const submit = new Event("submit", { bubbles: true, cancelable: true });
      form.dispatchEvent(submit);
      assert.equal(submit.defaultPrevented, true);
    }
    assert.equal(form.elements.body.value, "Unsent authored draft");
    assert.equal(dom.window.localStorage.length, 0);
  } finally {
    dom.window.close();
  }
});
test("a failed refresh blocks mutation and panel close can retry without resubmitting mail", async () => {
  let calls = 0;
  const dom = fixture(async () => {
    calls++;
    if (calls === 1) throw new Error("offline");
    return {
      ok: true,
      json: async () => ({
        user: { id: "owner" },
        csrfToken: csrf,
        expiresAt: expiry,
      }),
    };
  });
  try {
    dom.window.dispatchEvent(new dom.window.Event("mailspace:session-rotated"));
    await settle();
    dom.window.dispatchEvent(new dom.window.Event("mailspace:panel-closed"));
    await settle();
    assert.equal(calls, 2);
    assert.equal(
      dom.window.document.querySelector('[name="csrf"]').value,
      csrf,
    );
  } finally {
    dom.window.close();
  }
});

test("pagehide without a timeline does not leave a native sign-in or Security form blocked", () => {
  const dom = fixture(async () => {
    throw new Error("unexpected fetch");
  });
  try {
    const form = dom.window.document.querySelector("form");
    dom.window.document.body.append(form);
    dom.window.document.getElementById("timeline-shell").remove();
    dom.window.dispatchEvent(new dom.window.Event("pagehide"));
    const submit = new dom.window.Event("submit", {
      bubbles: true,
      cancelable: true,
    });
    form.dispatchEvent(submit);
    assert.equal(submit.defaultPrevented, false);
  } finally {
    dom.window.close();
  }
});
