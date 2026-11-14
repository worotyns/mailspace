// SPDX-License-Identifier: AGPL-3.0-only
import "fake-indexeddb/auto";
import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { InlineEncryption } from "../../internal/webui/assets/crypto/timeline-crypto.mjs";
function fixture(vault) {
  const dom = new JSDOM(
    `<div data-user-id="SYNTHETIC_DIALOG_USER_123" data-session-expires="${Date.now() + 60000}"><form><textarea>Unsent private draft</textarea><button data-send></button><p data-reply-status></p></form><dialog id="inline-key-dialog"><select></select><input type="password"><p id="inline-key-status"></p><button id="inline-key-unlock"></button></dialog></div>`,
  );
  const saved = new Map(
    ["window", "document", "isSecureContext", "AbortController"].map((name) => [
      name,
      Object.getOwnPropertyDescriptor(globalThis, name),
    ]),
  );
  Object.assign(globalThis, {
    window: dom.window,
    document: dom.window.document,
    isSecureContext: true,
    AbortController: dom.window.AbortController,
  });
  const shell = dom.window.document.querySelector("div"),
    dialog = shell.querySelector("dialog");
  dialog.showModal = () => {
    dialog.open = true;
  };
  dialog.close = () => {
    dialog.open = false;
    dialog.dispatchEvent(new dom.window.Event("close"));
  };
  const controller = new InlineEncryption(shell, shell.querySelector("form"), {
    vaultFactory: () => vault,
  });
  return {
    controller,
    dialog,
    document: dom.window.document,
    close() {
      controller.dispose();
      dom.window.close();
      for (const [name, value] of saved) {
        if (value) Object.defineProperty(globalThis, name, value);
        else delete globalThis[name];
      }
    },
  };
}
test("unlock modal opens before asynchronous key listing and cancel never reopens it", async () => {
  let finish;
  const pending = new Promise((resolve) => {
    finish = resolve;
  });
  const f = fixture({
    list: () => pending,
    lock() {},
    close() {},
    sessionMode: "page",
    locked: true,
  });
  try {
    const opened = f.controller.open();
    assert.equal(f.dialog.open, true);
    assert.match(f.dialog.textContent, /Loading local keys/);
    assert.equal(f.document.querySelector("#inline-key-unlock").disabled, true);
    f.dialog.close();
    finish({ keys: [], locked: true });
    await opened;
    assert.equal(f.dialog.open, false);
    assert.equal(
      f.document.querySelector("textarea").value,
      "Unsent private draft",
    );
  } finally {
    f.close();
  }
});
test("failed browser worker leaves a visible unlock error and never submits or downgrades", async () => {
  const f = fixture({
    list: async () => {
      throw new Error("unavailable");
    },
    lock() {},
    close() {},
    sessionMode: "shared",
    locked: true,
  });
  try {
    await assert.rejects(f.controller.open());
    assert.equal(f.dialog.open, true);
    assert.match(f.dialog.textContent, /Local key session unavailable/);
    assert.equal(f.document.querySelector("#inline-key-unlock").disabled, true);
    assert.equal(
      f.document.querySelector("textarea").value,
      "Unsent private draft",
    );
  } finally {
    f.close();
  }
});
