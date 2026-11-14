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
