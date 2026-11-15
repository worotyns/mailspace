// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const source = await readFile(
  new URL("../crypto/read-state.mjs", import.meta.url),
  "utf8",
);
function fixture(html) {
  const dom = new JSDOM(html, {
    url: "https://mail.example.test/",
    runScripts: "outside-only",
  });
  const calls = [];
  const state = { fail: false };
  dom.window.fetch = async (url, options = {}) => {
    calls.push({
      url: String(url),
      options,
      body: options.body
        ? Object.fromEntries(new URLSearchParams(options.body))
        : {},
    });
    return { ok: !state.fail };
  };
  dom.window.eval(
    source.replace(/\bexport\s+/g, "") +
      "\nwindow.__readState = { markReaderRead, markMessageRead };",
  );
  return { dom, calls, state };
}
const READER = `<main id="secure-mail" data-seen-writes="true" data-seen-unread="true" data-seen-reference="reference" data-csrf="csrf"></main>`;

test("opening the reader marks an unread message read exactly once through the bounded endpoint", async () => {
  const { dom, calls } = fixture(READER);
  try {
    const root = dom.window.document.getElementById("secure-mail");
    assert.equal(await dom.window.__readState.markReaderRead(root), true);
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, "/messages/seen");
    assert.equal(calls[0].options.method, "POST");
    assert.equal(calls[0].options.credentials, "same-origin");
    assert.equal(calls[0].options.headers["X-Mailspace-Background"], "1");
    assert.deepEqual(calls[0].body, {
      csrf: "csrf",
      reference: "reference",
      seen: "1",
    });
    assert.equal(await dom.window.__readState.markReaderRead(root), false);
    assert.equal(calls.length, 1);
  } finally {
    dom.window.close();
  }
});

test("a disabled capability, missing reference or already read row never writes", async () => {
  for (const html of [
    `<main id="secure-mail" data-seen-writes="false" data-seen-unread="true" data-seen-reference="reference"></main>`,
    `<main id="secure-mail" data-seen-writes="true" data-seen-unread="true"></main>`,
    `<main id="secure-mail" data-seen-writes="true" data-seen-unread="false" data-seen-reference="reference"></main>`,
  ]) {
    const { dom, calls } = fixture(html);
    try {
      const root = dom.window.document.getElementById("secure-mail");
      assert.equal(await dom.window.__readState.markReaderRead(root), false);
      assert.equal(calls.length, 0);
    } finally {
      dom.window.close();
    }
  }
});

test("a failed automatic write stays retryable and never claims success", async () => {
  const { dom, calls, state } = fixture(READER);
  try {
    const root = dom.window.document.getElementById("secure-mail");
    state.fail = true;
    assert.equal(await dom.window.__readState.markReaderRead(root), false);
    assert.equal(calls.length, 1);
    state.fail = false;
    assert.equal(await dom.window.__readState.markReaderRead(root), true);
    assert.equal(calls.length, 2);
  } finally {
    dom.window.close();
  }
});

test("an inline decrypted preview reuses its scoped form and leaves read rows alone", async () => {
  const { dom, calls } = fixture(
    `<article data-message-key="key"><span data-message-seen><form action="/messages/seen"><input name="csrf" value="csrf"><input name="reference" value="reference"><input name="seen" value="1"></form></span></article>`,
  );
  try {
    const article = dom.window.document.querySelector("article");
    assert.equal(await dom.window.__readState.markMessageRead(article), true);
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, "/messages/seen");
    assert.deepEqual(calls[0].body, {
      csrf: "csrf",
      reference: "reference",
      seen: "1",
    });
    assert.equal(await dom.window.__readState.markMessageRead(article), false);
    assert.equal(calls.length, 1);
  } finally {
    dom.window.close();
  }
  const read = fixture(
    `<article data-message-key="key"><span data-message-seen><form action="/messages/seen"><input name="reference" value="reference"><input name="seen" value="0"></form></span></article>`,
  );
  try {
    const article = read.dom.window.document.querySelector("article");
    assert.equal(
      await read.dom.window.__readState.markMessageRead(article),
      false,
    );
    assert.equal(read.calls.length, 0);
  } finally {
    read.dom.window.close();
  }
});
