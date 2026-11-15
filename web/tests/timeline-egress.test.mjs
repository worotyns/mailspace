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
  const recipientKeys = [];
  for (const address of [
    "second@example.test",
    "copy@example.test",
    "blind@example.test",
  ]) {
    const recipientKey = await local.generate(address, address, password);
    await local.trust(
      address,
      recipientKey.fingerprint,
      recipientKey.fingerprint,
    );
    recipientKeys.push(recipientKey);
  }
  await local.trust("inline@example.test", key.fingerprint, key.fingerprint);
  await local.unlock(key.fingerprint, password);
  const dom = new JSDOM(
    `<div id="shell" data-user-id="${user}" data-session-expires="${Date.now() + 60000}" data-csrf="csrf"><form data-from="inline@example.test" data-subject="Visible subject"><input name="account" value="account"/><input name="reference" value="typed-parent"/><input name="request_id" value="SYNTHETIC_INLINE_NONCE"/><input name="to" value="inline@example.test, Second &lt;second@example.test&gt;"/><input name="cc" value="Copy &lt;copy@example.test&gt;"/><input name="bcc" value="blind@example.test"/><textarea></textarea><button id="inline-send" data-send></button><p id="inline-reply-status" data-reply-status></p></form><dialog id="inline-key-dialog"><select></select><input type="password"/><p id="inline-key-status"></p><button id="inline-key-unlock"></button></dialog></div>`,
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
    "CustomEvent",
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
      CustomEvent: dom.window.CustomEvent,
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
      assert.equal(
        payload.to,
        "inline@example.test, Second <second@example.test>",
      );
      assert.equal(payload.cc, "Copy <copy@example.test>");
      assert.equal(payload.bcc, "blind@example.test");
      const decrypted = await local.decryptSigned(
        await extractEncrypted(new TextEncoder().encode(payload.encryptedMime)),
        "inline@example.test",
      );
      assert.equal((await parseLocalMime(decrypted.data)).text.trim(), body);
      for (const recipientKey of recipientKeys) {
        await local.unlock(recipientKey.fingerprint, password);
        const copy = await local.decryptSigned(
          await extractEncrypted(
            new TextEncoder().encode(payload.encryptedMime),
          ),
          "inline@example.test",
        );
        assert.equal((await parseLocalMime(copy.data)).text.trim(), body);
      }
      return {
        ok: true,
        json: async () => ({
          location: "/outbox?account=account",
          state: "accepted",
        }),
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
    controller.select.value = key.fingerprint;
    controller.pass.value = password;
    await controller.unlock();
    assert.equal(controller.pass.value, "");
    form.querySelector("textarea").value = body;
    // Cc and Bcc are encryption recipients too; missing trust must block before
    // any ciphertext request, without downgrading or erasing the draft.
    for (const field of ["cc", "bcc"]) {
      const input = form.elements.namedItem(field),
        original = input.value;
      input.value = "untrusted@example.test";
      await controller.send();
      assert.equal(calls, 0);
      assert.equal(form.querySelector("textarea").value, body);
      assert.match(
        controller.status.textContent,
        /verified recipient or sender key/,
      );
      input.value = original;
    }
    await controller.send();
    assert.equal(calls, 1);
    assert.match(controller.status.textContent, /Mail server accepted/);
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

function routingFixture(routing, encrypt) {
  const dom = new JSDOM(
    `<div data-user-id="synthetic-routing" data-session-expires="${Date.now() + 60000}" data-csrf="csrf"><form data-from="sender@example.test"><input type="hidden" name="account" value="original-account"><input type="hidden" name="request_id" value="synthetic-routing-request"><input type="hidden" name="reference" value="selected-original"><input type="hidden" name="space" value="private-space"><input type="hidden" name="to"><input type="hidden" name="cc"><input type="hidden" name="bcc"><input name="subject" value="Topic"><textarea>PRIVATE_ROUTING_CANARY</textarea><button data-send></button><p data-reply-status></p></form><dialog id="inline-key-dialog"><select></select><input type="password"><p id="inline-key-status"></p><button id="inline-key-unlock"></button></dialog></div>`,
    { url: "https://mail.example.test/" },
  );
  const names = [
    "window",
    "document",
    "isSecureContext",
    "fetch",
    "AbortController",
    "setInterval",
    "clearInterval",
    "CustomEvent",
  ];
  const saved = new Map(
    names.map((name) => [
      name,
      Object.getOwnPropertyDescriptor(globalThis, name),
    ]),
  );
  Object.assign(globalThis, {
    window: dom.window,
    document: dom.window.document,
    isSecureContext: true,
    AbortController: dom.window.AbortController,
    setInterval: () => 0,
    clearInterval: () => {},
    CustomEvent: dom.window.CustomEvent,
  });
  const shell = dom.window.document.querySelector("div"),
    form = shell.querySelector("form");
  for (const [name, value] of Object.entries(routing))
    form.elements.namedItem(name).value = value;
  const dialog = shell.querySelector("dialog");
  dialog.close = () => {};
  let calls = 0;
  const controller = new InlineEncryption(shell, form, {
    vaultFactory: () => ({
      locked: false,
      sessionMode: "shared",
      encryptSigned: encrypt,
      lock() {},
      close() {},
    }),
  });
  globalThis.fetch = async (url, options) => {
    calls++;
    return {
      ok: true,
      json: async () => ({
        location: "/outbox?account=original-account",
        state: "accepted",
      }),
    };
  };
  return {
    dom,
    form,
    controller,
    get calls() {
      return calls;
    },
    close() {
      controller.dispose();
      dom.window.close();
      for (const [name, descriptor] of saved) {
        if (descriptor) Object.defineProperty(globalThis, name, descriptor);
        else delete globalThis[name];
      }
    },
  };
}

test("inline encryption rejects invalid or excessive recipients across To/Cc/Bcc before key work or egress", async () => {
  for (const routing of [
    { to: "", cc: "", bcc: "" },
    { to: "a@example.test", cc: "bad\r\nBcc: injected@example.test" },
    { to: "a@example.test", bcc: "bad\0@example.test" },
    { to: "a@example.test", cc: "é".repeat(4100) },
    { to: "a@example.test", bcc: "a".repeat(255) + "@example.test" },
    {
      to: Array.from({ length: 50 }, (_, i) => `to${i}@example.test`).join(","),
      cc: Array.from({ length: 50 }, (_, i) => `cc${i}@example.test`).join(","),
      bcc: "one-too-many@example.test",
    },
  ]) {
    let encrypted = 0;
    const f = routingFixture(routing, async () => {
      encrypted++;
      return "unused";
    });
    try {
      await f.controller.send();
      assert.equal(encrypted, 0);
      assert.equal(f.calls, 0);
      assert.equal(
        f.form.querySelector("textarea").value,
        "PRIVATE_ROUTING_CANARY",
      );
      assert.equal(f.controller.submitted, undefined);
      assert.equal(f.controller.button.disabled, false);
    } finally {
      f.close();
    }
  }
});

test("Bcc-only inline encryption preserves the routing snapshot and uses the same ciphertext-only contract", async () => {
  let payload;
  const f = routingFixture(
    { to: "", cc: "", bcc: '"Doe, Jane" <blind@example.test>' },
    async (bytes, from, recipients) => {
      assert.equal(from, "sender@example.test");
      assert.deepEqual(recipients, ["blind@example.test"]);
      assert.equal(
        (await parseLocalMime(bytes)).text.trim(),
        "PRIVATE_ROUTING_CANARY",
      );
      f.form.elements.account.value = "changed-account";
      f.form.elements.bcc.value = "changed@example.test";
      return "-----BEGIN PGP MESSAGE-----\nSYNTHETIC_OPAQUE_RESULT\n-----END PGP MESSAGE-----";
    },
  );
  try {
    globalThis.fetch = async (url, options) => {
      assert.equal(url, "/api/outbox/encrypted");
      payload = JSON.parse(options.body);
      assert.equal(options.body.includes("PRIVATE_ROUTING_CANARY"), false);
      return {
        ok: true,
        json: async () => ({
          location: "/outbox?account=original-account",
          state: "accepted",
        }),
      };
    };
    await f.controller.send();
    assert.equal(payload.account, "original-account");
    assert.equal(payload.bcc, '"Doe, Jane" <blind@example.test>');
    assert.equal(payload.to, "");
    assert.equal(payload.cc, "");
    assert.equal(payload.reference, "selected-original");
    assert.equal(payload.space, "private-space");
    assert.equal(payload.body, undefined);
  } finally {
    f.close();
  }
});
