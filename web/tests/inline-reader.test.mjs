// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import {
  InlineReader,
  renderQuotedText,
} from "../../internal/webui/assets/crypto/inline-reader.mjs";
import { textEntity } from "../../internal/webui/assets/crypto/mime.mjs";
const tick = () => new Promise((resolve) => setImmediate(resolve));
function fixture(count = 1, options = {}) {
  const html = `<button data-local-key-lock>Lock local keys</button><label data-inline-reader-controls hidden><input type="checkbox" data-local-autopreview>Autopreview</label><p data-local-key-state></p><section id="shell" data-user-id="SYNTHETIC_INLINE_READER_USER" data-session-expires="${Date.now() + 60000}">${Array.from({ length: count }, (_, i) => `<article data-message-reference="ciphertext-${i}" data-message-person="sender@example.test"><div data-inline-encrypted hx-disable><a href="/messages/read?reference=ciphertext-${i}" data-inline-decrypt>Preview locally</a><div data-local-preview hidden></div></div></article>`).join("")}</section>`;
  const dom = new JSDOM(html, {
    pretendToBeVisual: true,
    url: "https://mail.example.test",
  });
  dom.window.HTMLDialogElement.prototype.showModal = function () {
    this.open = true;
  };
  dom.window.HTMLDialogElement.prototype.close = function (value = "") {
    if (!this.open) return;
    this.returnValue = value;
    this.open = false;
    this.dispatchEvent(new dom.window.Event("close"));
  };
  const document = dom.window.document,
    requests = [];
  const vault = new (class extends dom.window.EventTarget {
    locked = false;
    fingerprint = "A".repeat(40);
    sessionMode = "shared";
    shared = true;
    locks = 0;
    closes = 0;
    async list() {
      return {
        keys: [
          {
            private: true,
            fingerprint: this.fingerprint,
            identities: ["Synthetic sender"],
          },
        ],
        locked: this.locked,
        fingerprint: this.locked ? null : this.fingerprint,
      };
    }
    async unlock() {
      this.locked = false;
      return this.fingerprint;
    }
    async decryptSigned() {
      return {
        provenance: "client-decrypted",
        data: textEntity(
          'Current answer\n> quoted <img src="https://tracker.example.test/">\nVisible reply\nPRIVATE_INLINE_CANARY',
        ),
        signatures: [{ status: "valid", trustedSender: true }],
      };
    }
    lock() {
      const was = this.locked;
      this.locked = true;
      this.locks++;
      if (!was) this.dispatchEvent(new dom.window.Event("lock"));
    }
    async close() {
      this.closes++;
    }
  })();
  const shell = document.getElementById("shell");
  const reader = new InlineReader(shell, {
    canDecrypt: true,
    vaultFactory: () => vault,
    extractEncrypted: async (bytes) => bytes,
    fetch: async (url, request) => {
      requests.push({ url, request });
      assert.match(url, /^\/api\/messages\/raw\?reference=ciphertext-/);
      assert.equal(request.body, undefined);
      return {
        ok: true,
        arrayBuffer: async () => new Uint8Array([1, 2, 3]).buffer,
      };
    },
    ...options,
  });
  return {
    dom,
    document,
    shell,
    reader,
    vault,
    requests,
    entry: shell.querySelector("[data-inline-encrypted]"),
    close: () => {
      reader.dispose();
      dom.window.close();
    },
  };
}
test("inline decrypt is inert DOM-only text, folds quotes and only fetches ciphertext", async () => {
  const f = fixture();
  try {
    await f.reader.preview(f.entry);
    const output = f.entry.querySelector("[data-local-preview]");
    assert.equal(output.hidden, false);
    assert.match(output.textContent, /PRIVATE_INLINE_CANARY/);
    assert.match(output.textContent, /verified sender fingerprint/);
    assert.equal(output.querySelector("details").open, false);
    assert.match(output.querySelector("details").textContent, /quoted/);
    assert.equal(
      output.querySelectorAll("img,script,iframe,textarea,input,form").length,
      0,
    );
    assert.equal(f.requests.length, 1);
    assert.equal(f.entry.hasAttribute("hx-disable"), true);
    assert.equal(
      [...f.document.querySelectorAll("*")].some((el) =>
        [...el.attributes].some((attr) =>
          attr.value.includes("PRIVATE_INLINE_CANARY"),
        ),
      ),
      false,
    );
    f.reader.clearPrivate();
    assert.equal(output.textContent, "");
    assert.equal(output.hidden, true);
    assert.equal(
      f.vault.locks,
      0,
      "clearing UI must not lock the shared session",
    );
  } finally {
    f.close();
  }
  assert.equal(f.vault.closes, 1);
  assert.equal(f.vault.locks, 0);
});
test("a late decrypt after worker lock cannot recreate private content", async () => {
  const f = fixture();
  let release, started;
  const ready = new Promise((resolve) => {
    started = resolve;
  });
  f.vault.decryptSigned = () => {
    started();
    return new Promise((resolve) => {
      release = resolve;
    });
  };
  try {
    const pending = f.reader.preview(f.entry);
    await ready;
    f.vault.lock();
    release({
      provenance: "client-decrypted",
      data: textEntity("LATE_PRIVATE_CANARY"),
      signatures: [],
    });
    await pending;
    assert.doesNotMatch(f.document.body.textContent, /LATE_PRIVATE_CANARY/);
    assert.equal(f.entry.querySelector("[data-local-preview]").hidden, true);
  } finally {
    f.close();
  }
});
test("unlock action disables immediately and cancellation rejects late rendering", async () => {
  const f = fixture();
  f.vault.locked = true;
  let release;
  f.vault.unlock = () =>
    new Promise((resolve) => {
      release = () => {
        f.vault.locked = false;
        resolve();
      };
    });
  try {
    await f.reader.preview(f.entry);
    assert.equal(f.reader.dialog.open, true);
    const pending = f.reader.unlock();
    assert.equal(f.reader.unlockButton.disabled, true);
    f.reader.cancelUnlock();
    f.reader.dialog.close();
    release();
    await pending;
    assert.equal(f.requests.length, 0);
    assert.equal(f.vault.locks, 1);
  } finally {
    f.close();
  }
});
test("autopreview is opt-in, visibility-limited and has at most two concurrent decrypts", async () => {
  const f = fixture(4);
  const entries = [...f.shell.querySelectorAll("[data-inline-encrypted]")];
  const pending = [];
  let running = 0,
    maximum = 0;
  for (let i = 0; i < entries.length; i++)
    entries[i].getBoundingClientRect = () => ({
      width: 300,
      height: 80,
      top: i === 3 ? 10000 : 100,
      bottom: i === 3 ? 10080 : 180,
    });
  f.vault.decryptSigned = () =>
    new Promise((resolve) => {
      running++;
      maximum = Math.max(maximum, running);
      pending.push(() => {
        running--;
        resolve({
          provenance: "client-decrypted",
          data: textEntity("Automatic local text"),
          signatures: [],
        });
      });
    });
  try {
    await tick();
    assert.equal(f.requests.length, 0);
    f.reader.automatic = true;
    await f.reader.refreshAutomatic();
    await tick();
    assert.equal(f.requests.length, 2);
    assert.equal(maximum, 2);
    pending.shift()();
    await tick();
    await tick();
    assert.equal(f.requests.length, 3);
    assert.equal(maximum, 2);
    while (pending.length) pending.shift()();
    await tick();
    await tick();
    assert.equal(entries[3].querySelector("[data-local-preview]").hidden, true);
    Object.defineProperty(f.document, "hidden", {
      value: true,
      configurable: true,
    });
    f.document.dispatchEvent(new f.dom.window.Event("visibilitychange"));
    assert.equal(
      f.document.querySelectorAll("[data-local-preview]:not([hidden])").length,
      0,
    );
    assert.equal(
      f.vault.locks,
      0,
      "hiding clears content but retains the bounded worker session",
    );
  } finally {
    f.close();
    while (pending.length) pending.shift()();
  }
});
test("quote rendering keeps inline replies visible and limits pathological node counts", () => {
  const dom = new JSDOM('<div id="text"></div>');
  const output = dom.window.document.getElementById("text");
  try {
    renderQuotedText(
      output,
      "> first\n>> nested\nVisible reply\n> second",
      dom.window.document,
    );
    assert.equal(output.querySelectorAll("details").length, 2);
    assert.match(
      output.querySelector(":scope > p").textContent,
      /Visible reply/,
    );
    renderQuotedText(
      output,
      "> quoted\nvisible\n".repeat(1000),
      dom.window.document,
    );
    assert.equal(output.querySelectorAll("p").length, 1);
  } finally {
    dom.window.close();
  }
});

test("older encrypted rows join the existing local reader without clearing a mounted preview", async () => {
  const f = fixture();
  try {
    await f.reader.preview(f.entry);
    const output = f.entry.querySelector("[data-local-preview]"),
      retained = output.firstChild;
    const row = f.document.createElement("article");
    row.dataset.messageReference = "ciphertext-older";
    row.dataset.messagePerson = "sender@example.test";
    row.innerHTML =
      '<div data-inline-encrypted hx-disable><a data-inline-decrypt href="/messages/read?reference=ciphertext-older">Preview locally</a><div data-local-preview hidden></div></div>';
    f.shell.prepend(row);
    f.reader.extend();
    f.reader.extend();
    assert.equal(f.reader.entries.length, 2);
    assert.equal(output.firstChild, retained);
    await f.reader.preview(row.querySelector("[data-inline-encrypted]"));
    assert.match(
      row.querySelector("[data-local-preview]").textContent,
      /PRIVATE_INLINE_CANARY/,
    );
    f.reader.clearPrivate();
    assert.equal(
      row.querySelector("[data-local-preview]").childNodes.length,
      0,
    );
    assert.equal(output.childNodes.length, 0);
  } finally {
    f.close();
  }
});
