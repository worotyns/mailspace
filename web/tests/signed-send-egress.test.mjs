// SPDX-License-Identifier: AGPL-3.0-only
import "fake-indexeddb/auto";
import test from "node:test";
import assert from "node:assert/strict";
import { LocalVault } from "../../internal/webui/assets/crypto/vault.mjs";
import { VaultStore } from "../../internal/webui/assets/crypto/store.mjs";
import { extractSigned } from "../../internal/webui/assets/crypto/signed-mime.mjs";
import { parseLocalMime } from "../../internal/webui/assets/crypto/mime.mjs";

test("authored signing needs explicit consent, submits no secrets, and never retries an uncertain send", async () => {
  const user = "SYNTHETIC_SIGNED_SEND_EGRESS_USER";
  const password = "Synthetic signed-only egress passphrase";
  const from = "signed-egress@example.test";
  const author = new LocalVault(new VaultStore(user));
  const key = await author.generate("Signed sender", from, password);
  await author.trust(from, key.fingerprint, key.fingerprint);
  await author.close();
  const listeners = new Map(),
    elements = new Map();
  let focused,
    uploads = 0,
    submitted;
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
        replaceChildren() {},
        setAttribute(name, value) {
          this.attributes.set(name, value);
        },
        removeAttribute(name) {
          this.attributes.delete(name);
        },
        focus() {
          focused = id;
        },
        remove() {},
      });
    return elements.get(id);
  };
  element("secure-mail").dataset = {
    userId: user,
    sessionExpires: String(Date.now() + 60_000),
    read: "false",
    account: "SYNTHETIC_SIGNED_ACCOUNT",
    from,
    csrf: "SYNTHETIC_CSRF",
    requestId: "SYNTHETIC_SIGNED_REQUEST_12345",
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
  try {
    globalThis.document = {
      getElementById: element,
      createElement: () => element(`dynamic-${elements.size}`),
      body: element("body"),
      addEventListener() {},
    };
    globalThis.window = { addEventListener() {} };
    globalThis.isSecureContext = true;
    globalThis.setInterval = () => 0;
    globalThis.location = {};
    globalThis.fetch = async (url, request) => {
      uploads++;
      assert.equal(url, "/api/outbox/signed");
      assert.equal(request.headers["X-CSRF-Token"], "SYNTHETIC_CSRF");
      for (const canary of [
        password,
        "BEGIN PGP PRIVATE KEY BLOCK",
        "decryptedMime",
      ])
        assert.equal(request.body.includes(canary), false);
      submitted = JSON.parse(request.body);
      assert.deepEqual(
        Object.keys(submitted).sort(),
        [
          "account",
          "requestId",
          "to",
          "cc",
          "bcc",
          "subject",
          "replyFolder",
          "replyUid",
          "replyValidity",
          "signedMime",
          "provenance",
          "allowPlaintext",
        ].sort(),
      );
      assert.equal(submitted.provenance, "authored");
      assert.equal(submitted.allowPlaintext, true);
      assert.equal(submitted.replyFolder, "INBOX");
      assert.equal(submitted.replyUid, 7);
      assert.equal(submitted.replyValidity, 9);
      const signed = await extractSigned(
        new TextEncoder().encode(submitted.signedMime),
      );
      const checker = new LocalVault(new VaultStore(user));
      try {
        const verification = await checker.verifyDetached(
          signed.data,
          signed.signature,
          from,
        );
        assert.equal(verification.signatures[0].trustedSender, true);
        const mime = await parseLocalMime(signed.data);
        assert.equal(mime.text, "Authored Unicode zażółć");
        assert.deepEqual(
          new Uint8Array(mime.attachments[0].content),
          new Uint8Array([0, 255, 13, 10]),
        );
      } finally {
        await checker.close();
      }
      throw new Error("Synthetic ambiguous connection loss after submission");
    };
    await import(
      "../../internal/webui/assets/crypto/mail-ui.mjs?signed-egress"
    );
    element("mail-passphrase").value = password;
    await listeners.get("mail-unlock:click")();
    assert.equal(element("mail-passphrase").value, "");
    assert.equal(element("secure-encrypt").checked, true);
    element("secure-sign-only").checked = true;
    element("secure-encrypt").checked = false;
    listeners.get("secure-sign-only:change")();
    assert.equal(element("secure-plaintext-consent-row").hidden, false);
    // No recipient key is imported: signing only must still succeed.
    element("secure-to").value = "recipient-without-key@example.test";
    element("secure-subject").value = "Visible subject";
    element("secure-body").value = "Authored Unicode zażółć";
    element("secure-files").files = [
      {
        name: "octets.bin",
        size: 4,
        arrayBuffer: async () => new Uint8Array([0, 255, 13, 10]).buffer,
      },
    ];
    await listeners.get("secure-send:click")();
    assert.equal(uploads, 0);
    assert.equal(focused, "secure-plaintext-consent");
    assert.match(element("secure-status").textContent, /Approve sending/);
    assert.equal(element("secure-body").value, "Authored Unicode zażółć");
    element("secure-plaintext-consent").checked = true;
    element("secure-mail").dataset.encryptionRequired = "true";
    await listeners.get("secure-send:click")();
    assert.equal(uploads, 0);
    assert.match(element("secure-status").textContent, /Keep encryption/);
    element("secure-mail").dataset.encryptionRequired = "false";
    // Any protection change revokes the old plaintext consent.
    listeners.get("secure-sign-only:change")();
    assert.equal(element("secure-plaintext-consent").checked, false);
    element("secure-plaintext-consent").checked = true;
    await listeners.get("secure-send:click")();
    assert.ok(submitted);
    assert.equal(uploads, 1);
    assert.match(element("secure-status").textContent, /check Outbox/i);
    assert.equal(element("secure-compose-fields").disabled, true);
    await listeners.get("secure-send:click")();
    assert.equal(uploads, 1);
    listeners.get("mail-lock:click")();
    assert.equal(element("secure-body").value, "");
    assert.equal(element("secure-sign-only").checked, false);
    assert.equal(element("secure-plaintext-consent").checked, false);
  } finally {
    listeners.get("mail-lock:click")?.();
    for (const [name, value] of Object.entries(previous))
      if (value === undefined) delete globalThis[name];
      else globalThis[name] = value;
  }
});
