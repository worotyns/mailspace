// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { ClientSelection } from "../../internal/webui/assets/crypto/client-selection.mjs";
test("decrypted selection requires explicit per-excerpt consent and never uploads automatically or persists full mail", async () => {
  const dom = new JSDOM(
    `<div id="root" data-session-expires="${Date.now() + 60000}" data-classify-reference="owned-encrypted-source" data-classify-provider="Synthetic provider" data-csrf="csrf"><p id="decrypted-text">FULL_PRIVATE_MESSAGE_CANARY</p><p id="secure-status"></p><button id="selection-classify-open"></button><dialog id="selection-classify-dialog"><textarea id="selection-classify-excerpt"></textarea><input type="checkbox" id="selection-classify-consent"/><button id="selection-classify-send"></button><button id="selection-classify-close"></button><p id="selection-classify-status"></p><div id="selection-classify-result"></div></dialog></div>`,
    { url: "http://127.0.0.1:8080" },
  );
  const names = ["window", "document", "fetch", "AbortController"];
  const saved = new Map(
    names.map((name) => [
      name,
      Object.getOwnPropertyDescriptor(globalThis, name),
    ]),
  );
  let controller,
    calls = 0;
  try {
    Object.assign(globalThis, {
      window: dom.window,
      document: dom.window.document,
      AbortController: dom.window.AbortController,
    });
    const dialog = document.querySelector("dialog");
    dialog.showModal = () => {
      dialog.open = true;
    };
    dialog.close = () => {
      if (dialog.open) {
        dialog.open = false;
        dialog.dispatchEvent(new dom.window.Event("close"));
      }
    };
    globalThis.fetch = async (url, request) => {
      calls++;
      assert.equal(url, "/api/automation/client-selection");
      const body = JSON.parse(request.body);
      assert.equal(body.excerpt, "APPROVED_SHORT_EXCERPT");
      assert.equal(body.consent.approved, true);
      assert.equal(body.consent.provider, "Synthetic provider");
      assert.equal(body.consent.purpose, "classify-selected-excerpt");
      assert.equal(request.body.includes("FULL_PRIVATE_MESSAGE_CANARY"), false);
      assert.equal(request.body.includes("PRIVATE KEY"), false);
      assert.equal(request.headers["X-CSRF-Token"], "csrf");
      return {
        ok: true,
        json: async () => ({
          id: "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
          spaces: [
            { id: "space", name: "<script>hostile model label</script>" },
          ],
          confidence: 0.9,
        }),
      };
    };
    controller = new ClientSelection(document.getElementById("root"));
    controller.enable();
    controller.open();
    assert.equal(calls, 0);
    assert.equal(controller.excerpt.value, "");
    assert.equal(controller.excerpt.name, "");
    assert.equal(controller.excerpt.closest("form"), null);
    controller.excerpt.value = "APPROVED_SHORT_EXCERPT";
    await controller.send();
    assert.equal(calls, 0);
    controller.consent.checked = true;
    await controller.send();
    assert.equal(calls, 1);
    assert.equal(controller.excerpt.value, "");
    assert.equal(controller.result.querySelector("script"), null);
    assert.ok(
      controller.result.textContent.includes(
        "<script>hostile model label</script>",
      ),
    );
    await controller.send();
    assert.equal(calls, 1);
    controller.clear();
    assert.equal(controller.excerpt.value, "");
    assert.equal(controller.consent.checked, false);
    assert.equal(controller.result.textContent, "");
    assert.equal(dialog.open, false);
    // An old response must not clear a newly reviewed excerpt after close/reopen.
    let finishOldRequest;
    globalThis.fetch = () =>
      new Promise((resolve) => {
        finishOldRequest = resolve;
      });
    controller.enable();
    controller.open();
    controller.excerpt.value = "FIRST_APPROVED_EXCERPT";
    controller.consent.checked = true;
    const pending = controller.send();
    controller.clear();
    controller.enable();
    controller.open();
    controller.excerpt.value = "NEW_UNSUBMITTED_EXCERPT";
    finishOldRequest({
      ok: true,
      json: async () => ({
        id: "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
        spaces: [],
        confidence: 1,
      }),
    });
    await pending;
    assert.equal(controller.excerpt.value, "NEW_UNSUBMITTED_EXCERPT");
    assert.equal(controller.result.textContent, "");
  } finally {
    controller?.clear();
    dom.window.close();
    for (const [name, descriptor] of saved) {
      if (descriptor) Object.defineProperty(globalThis, name, descriptor);
      else delete globalThis[name];
    }
  }
});
