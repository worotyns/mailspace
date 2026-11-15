// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const source = await readFile(
  new URL("../crypto/security-ui.mjs", import.meta.url),
  "utf8",
);
const sessionSource = await readFile(
  new URL("../../internal/webui/assets/session.js", import.meta.url),
  "utf8",
);
function fixture() {
  const dom = new JSDOM(
    `<p tabindex="-1" data-security-feedback>Try a fresh code</p><div data-security-secret><code>synthetic-seed</code><img data-security-qr src="/security/authenticator/qr"></div><form method="post" action="/security/authenticator/disable"><input name="password" type="password"><fieldset data-factor-choice><input type="radio" name="proof_type" value="totp" checked><input type="radio" name="proof_type" value="recovery"><input name="code" data-factor-code></fieldset></form>`,
    { url: "https://mail.example.test/security", runScripts: "outside-only" },
  );
  const calls = [];
  dom.window.fetch = () => {
    throw new Error("security UI must not fetch");
  };
  dom.window.BroadcastChannel = class {
    addEventListener() {}
    postMessage(value) {
      calls.push(value);
    }
  };
  dom.window.eval(sessionSource);
  dom.window.eval(source);
  return { dom, calls };
}
test("factor selection keeps native radio semantics, clears the previous proof and focuses one code field", () => {
  const { dom } = fixture();
  try {
    const { document, Event } = dom.window;
    const code = document.querySelector("[data-factor-code]");
    assert.equal(code.inputMode, "numeric");
    code.value = "001234";
    const recovery = document.querySelector('[value="recovery"]');
    recovery.checked = true;
    recovery.dispatchEvent(new Event("change", { bubbles: true }));
    assert.equal(code.inputMode, "text");
    assert.equal(code.value, "");
    assert.equal(document.activeElement, code);
    assert.equal(
      new dom.window.FormData(document.querySelector("form")).get("proof_type"),
      "recovery",
    );
  } finally {
    dom.window.close();
  }
});
test("native security submit preserves password/code serialization while broadcasting only a lock", async () => {
  const { dom, calls } = fixture();
  try {
    const { document, Event, FormData } = dom.window;
    const form = document.querySelector("form");
    form.elements.password.value = "synthetic-password";
    form.elements.code.value = "001234";
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
    const data = new FormData(form);
    assert.equal(data.get("password"), "synthetic-password");
    assert.equal(data.get("code"), "001234");
    assert.deepEqual(JSON.parse(JSON.stringify(calls)), [{ type: "lock" }]);
    // Emulate a browser microtask checkpoint between native event callbacks:
    // the bubble lock still belongs to the same submit task.
    await Promise.resolve();
    dom.window.dispatchEvent(new Event("mailspace:lock"));
    assert.equal(form.elements.code.value, "001234");
    assert.equal(form.elements.password.value, "synthetic-password");
    const serialized = new FormData(form);
    assert.equal(serialized.get("password"), "synthetic-password");
    await new Promise((resolve) => dom.window.setTimeout(resolve, 0));
    dom.window.dispatchEvent(new Event("mailspace:lock"));
    assert.equal(form.elements.code.value, "");
    assert.equal(form.elements.password.value, "");
    assert.equal(
      document.querySelector("[data-security-secret]").textContent,
      "",
    );
    assert.equal(document.querySelector("[data-security-qr]"), null);
  } finally {
    dom.window.close();
  }
});
for (const event of ["pagehide", "mailspace:clear-private"])
  test(`${event} removes pairing and recovery material`, () => {
    const { dom } = fixture();
    try {
      dom.window.dispatchEvent(new dom.window.Event(event));
      assert.equal(
        dom.window.document.querySelector("[data-security-secret]").childNodes
          .length,
        0,
      );
      assert.equal(dom.window.localStorage.length, 0);
      assert.equal(dom.window.sessionStorage.length, 0);
    } finally {
      dom.window.close();
    }
  });
