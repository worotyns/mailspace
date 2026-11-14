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

test("standalone HTMX actions disable immediately and reject repeated clicks", async () => {
  const dom = new JSDOM(
    '<button hx-post="/spaces">Assign</button><a href="/thread" hx-get="/thread">Thread</a>',
    { runScripts: "outside-only" },
  );
  try {
    dom.window.eval(
      await readFile(
        new URL("../../internal/webui/assets/forms.js", import.meta.url),
        "utf8",
      ),
    );
    for (const elt of dom.window.document.querySelectorAll("button,a")) {
      elt.dispatchEvent(
        new dom.window.CustomEvent("htmx:beforeRequest", {
          bubbles: true,
          detail: { elt },
        }),
      );
      assert.equal(elt.getAttribute("aria-busy"), "true");
      assert.equal(
        elt.tagName === "A" ? elt.getAttribute("aria-disabled") : elt.disabled,
        elt.tagName === "A" ? "true" : true,
      );
      assert.equal(
        elt.dispatchEvent(
          new dom.window.MouseEvent("click", {
            bubbles: true,
            cancelable: true,
          }),
        ),
        false,
      );
      elt.dispatchEvent(
        new dom.window.CustomEvent("htmx:afterRequest", {
          bubbles: true,
          detail: { elt },
        }),
      );
      assert.equal(elt.hasAttribute("aria-busy"), false);
    }
  } finally {
    dom.window.close();
  }
});

test("busy label swap restores the same scoped control without stealing focus", async () => {
  const dom = new JSDOM(
    '<form><button id="label-message-custom">Toggle</button></form><input id="other">',
    { runScripts: "outside-only" },
  );
  dom.window.eval(
    await readFile(
      new URL("../../internal/webui/assets/forms.js", import.meta.url),
      "utf8",
    ),
  );
  const { document, CustomEvent } = dom.window;
  const form = document.querySelector("form");
  document.querySelector("button").focus();
  form.dispatchEvent(
    new CustomEvent("htmx:beforeRequest", {
      bubbles: true,
      detail: { elt: form },
    }),
  );
  form.outerHTML =
    '<form><button id="label-message-custom">Updated</button></form>';
  document.body.focus();
  document.dispatchEvent(
    new CustomEvent("htmx:afterRequest", { detail: { elt: form } }),
  );
  assert.equal(document.activeElement.id, "label-message-custom");
  dom.window.close();
});
