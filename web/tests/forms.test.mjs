// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
test("busy form state preserves serialized explicit consent and restores after failures", async () => {
  const dom = new JSDOM(
    '<form><input name="body" value="Synthetic mail"><button name="allow_plaintext" value="1">Send unencrypted</button></form>',
    { runScripts: "outside-only" },
  );
  try {
    dom.window.eval(
      await readFile(
        new URL("../../internal/webui/assets/forms.js", import.meta.url),
        "utf8",
      ),
    );
    const form = dom.window.document.querySelector("form"),
      button = form.querySelector("button");
    const payload = new dom.window.FormData(form, button);
    form.dispatchEvent(
      new dom.window.CustomEvent("htmx:beforeRequest", {
        bubbles: true,
        detail: { elt: form },
      }),
    );
    assert.equal(payload.get("allow_plaintext"), "1");
    assert.equal(button.disabled, true);
    assert.equal(form.getAttribute("aria-busy"), "true");
    form.dispatchEvent(
      new dom.window.CustomEvent("htmx:afterRequest", {
        bubbles: true,
        detail: { elt: form, successful: false },
      }),
    );
    assert.equal(button.disabled, false);
    assert.equal(form.hasAttribute("aria-busy"), false);
  } finally {
    dom.window.close();
  }
});
