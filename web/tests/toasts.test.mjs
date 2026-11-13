// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
test("toasts are bounded inert text and remain dismissible", async () => {
  const dom = new JSDOM(
    '<button>Origin</button><p data-toast role="status">Saved safely</p>',
    { runScripts: "outside-only" },
  );
  try {
    const { window } = dom;
    window.eval(
      await readFile(
        new URL("../../internal/webui/assets/toasts.js", import.meta.url),
        "utf8",
      ),
    );
    const region = window.document.querySelector(
      '[aria-label="Notifications"]',
    );
    assert.match(region.textContent, /Saved safely/);
    assert.equal(window.document.querySelector("[data-toast]").hidden, true);
    for (let i = 0; i < 6; i++)
      window.dispatchEvent(
        new window.CustomEvent("mailspace:toast", {
          detail: { message: `<img src=x onerror=alert(1)> ${i}` },
        }),
      );
    assert.equal(region.children.length, 3);
    assert.equal(region.querySelector("img"), null);
    region.querySelector("button").click();
    assert.equal(region.children.length, 2);
  } finally {
    dom.window.close();
  }
});
