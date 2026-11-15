// SPDX-License-Identifier: AGPL-3.0-only
import "fake-indexeddb/auto";
import test from "node:test";
import assert from "node:assert/strict";
import { LocalVault } from "../../internal/webui/assets/crypto/vault.mjs";
import { VaultStore } from "../../internal/webui/assets/crypto/store.mjs";
import {
  composeEntity,
  encryptedEntity,
  extractEncrypted,
  parseLocalMime,
} from "../../internal/webui/assets/crypto/mime.mjs";

test("local quotes fetch only exact sources, survive only encrypted drafts, and never downgrade or return after lock", async () => {
  const user = "SYNTHETIC_QUOTE_EGRESS_USER",
    account = "SYNTHETIC_QUOTE_ACCOUNT",
    from = "quote-egress@example.test",
    password = "Synthetic quote egress passphrase";
  const canary = "PRIVATE_DECRYPTED_QUOTE_CANARY_1138",
    fileCanary = "PRIVATE_ORIGINAL_ATTACHMENT_BYTES";
  const author = new LocalVault(new VaultStore(user));
  const key = await author.generate("Quote sender", from, password);
  await author.trust(from, key.fingerprint, key.fingerprint);
  await author.unlock(key.fingerprint, password);
  const encrypted = await author.encryptSigned(
    composeEntity(canary, [
      { name: "original.bin", data: new TextEncoder().encode(fileCanary) },
    ]),
    from,
    [from],
  );
  await author.close();
  const raw = new TextEncoder().encode(
    `From: ${from}\r\nTo: ${from}\r\n${encryptedEntity(encrypted)}`,
  );
  const listeners = new Map(),
    windowListeners = new Map(),
    elements = new Map();
  const element = (id) => {
    if (!elements.has(id))
      elements.set(id, {
        id,
        value: "",
        checked: false,
        disabled: true,
        hidden: false,
        textContent: "",
        options: [],
        dataset: {},
        attributes: new Map(),
        addEventListener(type, handler) {
          listeners.set(`${id}:${type}`, handler);
        },
        append(option) {
          this.options.push(option);
          this.value ||= option.value;
        },
        replaceChildren() {
          this.options = [];
          this.value = "";
        },
        setAttribute(name, value) {
          this.attributes.set(name, value);
        },
        removeAttribute(name) {
          this.attributes.delete(name);
        },
        focus() {},
        remove() {},
      });
    return elements.get(id);
  };
  element("secure-mail").dataset = {
    userId: user,
    account,
    from,
    csrf: "SYNTHETIC_CSRF",
    read: "false",
    sessionExpires: String(Date.now() + 60_000),
    requestId: "SYNTHETIC_QUOTE_REQUEST_123456",
    replyFolder: "INBOX",
    replyUid: "7",
    replyValidity: "9",
    encryptionRequired: "false",
  };
  const previous = Object.fromEntries(
    [
      "document",
      "window",
      "isSecureContext",
      "fetch",
      "location",
      "setInterval",
    ].map((name) => [name, globalThis[name]]),
  );
  let gets = 0,
    posts = 0,
    delay,
    begin,
    observedSignal,
    missing = false;
  try {
    globalThis.document = {
      getElementById: element,
      createElement: () => element(`dynamic-${elements.size}`),
      body: element("body"),
      addEventListener() {},
    };
    globalThis.window = {
      addEventListener(type, handler) {
        windowListeners.set(type, handler);
      },
    };
    globalThis.isSecureContext = true;
    globalThis.location = {};
    globalThis.setInterval = () => 0;
    globalThis.fetch = async (url, request) => {
      if (url.startsWith("/api/mail/raw?")) {
        gets++;
        const query = new URL(url, "https://mail.example.test").searchParams;
        assert.deepEqual([...query.entries()].sort(), [
          ["account", account],
          ["folder", "INBOX"],
          ["uid", "7"],
          ["validity", "9"],
        ]);
        assert.equal(request.body, undefined);
        assert.equal(request.method, undefined);
        assert.equal(request.cache, "no-store");
        observedSignal = request.signal;
        begin?.();
        await delay;
        return { ok: !missing, arrayBuffer: async () => raw.buffer };
      }
      posts++;
      assert.equal(url, "/api/outbox/encrypted");
      for (const secret of [
        canary,
        fileCanary,
        password,
        "BEGIN PGP PRIVATE KEY BLOCK",
      ])
        assert.equal(request.body.includes(secret), false);
      const payload = JSON.parse(request.body);
      assert.equal(Object.hasOwn(payload, "signedMime"), false);
      assert.equal(Object.hasOwn(payload, "body"), false);
      assert.equal(Object.hasOwn(payload, "encryptionRequired"), false);
      assert.equal(payload.replyUid, 7);
      assert.equal(payload.replyValidity, 9);
      assert.equal(payload.replyFolder, "INBOX");
      const checker = new LocalVault(new VaultStore(user));
      try {
        await checker.unlock(key.fingerprint, password);
        const result = await checker.decryptSigned(
          await extractEncrypted(
            new TextEncoder().encode(payload.encryptedMime),
          ),
          from,
        );
        const mime = await parseLocalMime(result.data);
        assert.equal(mime.text, "Authored reply\n\n> " + canary);
        assert.equal(mime.attachments.length, 0);
      } finally {
        await checker.close();
      }
      throw new Error("Synthetic ambiguous send result");
    };
    await import("../../internal/webui/assets/crypto/mail-ui.mjs?quote-egress");
    assert.equal(gets, 0);
    const unlock = async () => {
      element("mail-passphrase").value = password;
      await listeners.get("mail-unlock:click")();
      assert.equal(element("mail-passphrase").value, "");
    };
    await unlock();
    element("secure-to").value = from;
    element("secure-body").value = "Pending author text";
    let release;
    delay = new Promise((resolve) => {
      release = resolve;
    });
    const began = new Promise((resolve) => {
      begin = resolve;
    });
    const pending = listeners.get("secure-quote-original:click")();
    await began;
    listeners.get("mail-lock:click")();
    assert.equal(observedSignal.aborted, true);
    release();
    await pending;
    assert.equal(element("secure-body").value, "");
    assert.equal(element("secure-quote-status").textContent, "");
    assert.equal(posts, 0);
    delay = undefined;
    begin = undefined;
    await unlock();
    element("secure-body").value = "Authored reply";
    missing = true;
    await listeners.get("secure-quote-original:click")();
    assert.equal(element("secure-body").value, "Authored reply");
    assert.equal(posts, 0);
    missing = false;
    await listeners.get("secure-quote-original:click")();
    assert.equal(element("secure-body").value, "Authored reply\n\n> " + canary);
    assert.equal(element("secure-sign-only").disabled, true);
    assert.equal(element("secure-encrypt").checked, true);
    assert.match(element("secure-quote-status").textContent, /verified sender/);
    const previousGets = gets;
    await listeners.get("secure-quote-original:click")();
    assert.equal(gets, previousGets);
    element("secure-sign-only").checked = true;
    element("secure-plaintext-consent").checked = true;
    await listeners.get("secure-send:click")();
    assert.equal(posts, 0);
    assert.match(element("secure-status").textContent, /Keep encryption/);
    element("secure-sign-only").checked = false;
    element("secure-encrypt").checked = true;
    listeners.get("secure-encrypt:change")();
    await listeners.get("draft-save:click")();
    const draftID = element("local-draft").value;
    assert.ok(draftID);
    listeners.get("mail-lock:click")();
    await unlock();
    element("local-draft").value = draftID;
    await listeners.get("draft-load:click")();
    assert.match(
      element("secure-quote-status").textContent,
      /not been rechecked/,
    );
    assert.equal(element("secure-body").value, "Authored reply\n\n> " + canary);
    assert.equal(element("secure-sign-only").disabled, true);
    element("secure-sign-only").checked = true;
    element("secure-plaintext-consent").checked = true;
    await listeners.get("secure-send:click")();
    assert.equal(posts, 0);
    for (const item of elements.values()) {
      assert.equal(
        JSON.stringify({
          dataset: item.dataset,
          attributes: [...item.attributes],
          text: item.textContent,
        }).includes(canary),
        false,
      );
      if (item.id !== "secure-body")
        assert.equal(item.value?.includes(canary) || false, false);
    }
    element("secure-sign-only").checked = false;
    element("secure-encrypt").checked = true;
    listeners.get("secure-encrypt:change")();
    await listeners.get("secure-send:click")();
    assert.equal(posts, 1);
    await listeners.get("secure-send:click")();
    assert.equal(posts, 1);
    listeners.get("mail-lock:click")();
    assert.equal(element("secure-body").value, "");
    assert.equal(element("secure-quote-status").textContent, "");
  } finally {
    listeners.get("mail-lock:click")?.();
    for (const [name, value] of Object.entries(previous))
      if (value === undefined) delete globalThis[name];
      else globalThis[name] = value;
  }
});
