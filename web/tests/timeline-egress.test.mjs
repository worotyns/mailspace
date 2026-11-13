// SPDX-License-Identifier: AGPL-3.0-only
import "fake-indexeddb/auto";
import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { LocalVault } from "../../internal/webui/assets/crypto/vault.mjs";
import { VaultStore } from "../../internal/webui/assets/crypto/store.mjs";
import { InlineEncryption } from "../../internal/webui/assets/crypto/timeline-crypto.mjs";
import {
  extractEncrypted,
  parseLocalMime,
} from "../../internal/webui/assets/crypto/mime.mjs";

test("inline encryption uploads ciphertext only, preserves typed parent, and prevents a second attempt", async () => {
  const user = "SYNTHETIC_INLINE_USER",
    password = "Synthetic inline passphrase 123",
    body = "PRIVATE_INLINE_BODY_CANARY";
  const local = new LocalVault(new VaultStore(user));
  const key = await local.generate(
    "Inline QA",
    "inline@example.test",
    password,
  );
  await local.trust("inline@example.test", key.fingerprint, key.fingerprint);
  await local.unlock(key.fingerprint, password);
  const dom = new JSDOM(
    `<div id="shell" data-user-id="${user}" data-session-expires="${Date.now() + 60000}" data-csrf="csrf"><form data-from="inline@example.test" data-subject="Visible subject"><input name="account" value="account"/><input name="reference" value="typed-parent"/><input name="request_id" value="SYNTHETIC_INLINE_NONCE"/><input name="to" value="inline@example.test"/><textarea></textarea><button id="inline-send"></button><p id="inline-reply-status"></p></form><dialog id="inline-key-dialog"><select></select><input type="password"/><p id="inline-key-status"></p><button id="inline-key-unlock"></button></dialog></div>`,
    { url: "http://127.0.0.1:8080" },
  );
  const names = [
    "window",
    "document",
    "isSecureContext",
    "location",
    "fetch",
    "AbortController",
    "setInterval",
    "clearInterval",
  ];
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
      isSecureContext: true,
      AbortController: dom.window.AbortController,
      setInterval: () => 0,
      clearInterval: () => {},
      location: { assign: () => {} },
    });
    globalThis.fetch = async (url, request) => {
      calls++;
      assert.equal(url, "/api/outbox/encrypted");
      for (const privateValue of [body, password, "PRIVATE KEY BLOCK"])
        assert.equal(request.body.includes(privateValue), false);
      const payload = JSON.parse(request.body);
      assert.deepEqual(
        Object.keys(payload).sort(),
        [
          "account",
          "requestId",
          "reference",
          "space",
          "to",
          "cc",
          "bcc",
          "subject",
          "encryptedMime",
        ].sort(),
      );
      assert.equal(payload.reference, "typed-parent");
      const decrypted = await local.decryptSigned(
        await extractEncrypted(new TextEncoder().encode(payload.encryptedMime)),
        "inline@example.test",
      );
      assert.equal((await parseLocalMime(decrypted.data)).text.trim(), body);
      return {
        ok: true,
        json: async () => ({ location: "/outbox?account=account" }),
      };
    };
    const shell = document.getElementById("shell"),
      form = shell.querySelector("form");
    const dialog = shell.querySelector("dialog");
    dialog.showModal = () => {
      dialog.open = true;
    };
    dialog.close = () => {
      dialog.open = false;
      dialog.dispatchEvent(new dom.window.Event("close"));
    };
    controller = new InlineEncryption(shell, form);
    await controller.open();
    controller.pass.value = password;
    await controller.unlock();
    assert.equal(controller.pass.value, "");
    form.querySelector("textarea").value = body;
    await controller.send();
    assert.equal(calls, 1);
    await controller.send();
    assert.equal(calls, 1);
    assert.equal(form.querySelector("textarea").value, "");
  } finally {
    controller?.dispose();
    await local.close();
    dom.window.close();
    for (const [name, descriptor] of saved) {
      if (descriptor) Object.defineProperty(globalThis, name, descriptor);
      else delete globalThis[name];
    }
  }
});
