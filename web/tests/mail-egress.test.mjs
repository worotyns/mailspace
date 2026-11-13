// SPDX-License-Identifier: AGPL-3.0-only
import "fake-indexeddb/auto";
import test from "node:test";
import assert from "node:assert/strict";
import * as pgp from "../../internal/webui/assets/vendor/openpgp/openpgp.min.mjs";
import { LocalDrafts } from "../../internal/webui/assets/crypto/drafts.mjs";
import { VaultStore } from "../../internal/webui/assets/crypto/store.mjs";
import {
  LocalVault,
  cryptoConfig,
} from "../../internal/webui/assets/crypto/vault.mjs";
import {
  extractEncrypted,
  encryptedEntity,
  textEntity,
} from "../../internal/webui/assets/crypto/mime.mjs";

test("actual composer event submits only ciphertext, never key/passphrase/plaintext", async () => {
  const id = "SYNTHETIC_EGRESS_BROWSER_USER",
    password = "Synthetic egress passphrase 123",
    body = "PRIVATE_HTTP_BODY_CANARY_<script>untrusted</script>";
  const vault = new LocalVault(new VaultStore(id));
  const generated = await vault.generate(
    "Egress QA",
    "egress@example.test",
    password,
  );
  await vault.trust(
    "egress@example.test",
    generated.fingerprint,
    generated.fingerprint,
  );
  await vault.close();
  const listeners = new Map(),
    elements = new Map();
  const element = (id) => {
    if (!elements.has(id))
      elements.set(id, {
        id,
        value: "",
        disabled: true,
        hidden: false,
        textContent: "",
        options: [],
        dataset: {},
        addEventListener(type, handler) {
          listeners.set(`${id}:${type}`, handler);
        },
        append(option) {
          this.options.push(option);
          this.value ||= option.value;
        },
        replaceChildren() {},
        setAttribute() {},
        close() {
          this.open = false;
        },
        remove() {},
      });
    return elements.get(id);
  };
  const root = element("secure-mail");
  root.dataset = {
    userId: id,
    sessionExpires: String(Date.now() + 60_000),
    read: "false",
    account: "SYNTHETIC_ACCOUNT",
    from: "egress@example.test",
    csrf: "SYNTHETIC_CSRF",
    requestId: "SYNTHETIC_ENCRYPTED_REQUEST_12345",
    replyFolder: "",
    replyValidity: "0",
    replyUid: "0",
  };
  const original = {
    document: globalThis.document,
    window: globalThis.window,
    isSecureContext: globalThis.isSecureContext,
    fetch: globalThis.fetch,
    location: globalThis.location,
    setInterval: globalThis.setInterval,
  };
  let submitted, navigated;
  try {
    globalThis.document = {
      getElementById: element,
      createElement: () => element(`dynamic-${elements.size}`),
      body: element("mock-body"),
      addEventListener() {},
    };
    globalThis.window = { addEventListener() {} };
    globalThis.isSecureContext = true;
    globalThis.setInterval = () => 0;
    globalThis.location = {
      assign: (value) => {
        navigated = value;
      },
    };
    globalThis.fetch = async (url, request) => {
      assert.equal(url, "/api/outbox/encrypted");
      assert.equal(request.method, "POST");
      assert.equal(request.headers["X-CSRF-Token"], "SYNTHETIC_CSRF");
      for (const canary of [
        body,
        password,
        "PRIVATE_FILE_CANARY",
        "PRIVATE_FILENAME_CANARY.bin",
        "BEGIN PGP PRIVATE KEY BLOCK",
      ])
        assert.equal(request.body.includes(canary), false);
      submitted = JSON.parse(request.body);
      assert.deepEqual(
        Object.keys(submitted).sort(),
        [
          "account",
          "bcc",
          "cc",
          "encryptedMime",
          "replyFolder",
          "replyUid",
          "replyValidity",
          "requestId",
          "subject",
          "to",
        ].sort(),
      );
      await extractEncrypted(new TextEncoder().encode(submitted.encryptedMime));
      return {
        ok: true,
        json: async () => ({ location: "/outbox?account=SYNTHETIC_ACCOUNT" }),
      };
    };
    await import("../../internal/webui/assets/crypto/mail-ui.mjs");
    element("mail-passphrase").value = password;
    await listeners.get("mail-unlock:click")();
    assert.equal(element("mail-passphrase").value, "");
    assert.equal(element("secure-compose-fields").disabled, false);
    for (const [field, value] of Object.entries({
      to: "egress@example.test",
      cc: "",
      bcc: "",
      subject: "Visible subject",
      body,
    }))
      element("secure-" + field).value = value;
    element("secure-files").files = [
      {
        name: "PRIVATE_FILENAME_CANARY.bin",
        size: 19,
        arrayBuffer: async () =>
          new TextEncoder().encode("PRIVATE_FILE_CANARY").buffer,
      },
    ];
    await listeners.get("secure-send:click")();
    assert.ok(submitted, "composer did not reach ciphertext submission");
    assert.equal(submitted.subject, "Visible subject");
    assert.equal(navigated, undefined, "sending must keep the current view");
    assert.equal(element("secure-body").value, "");
    assert.equal(element("secure-compose-fields").disabled, true);
    // Re-import the same shipped reader module with a fresh DOM, then prove
    // decryption has no request other than fetching the original ciphertext.
    const sender = new LocalVault(new VaultStore(id));
    await sender.unlock(generated.fingerprint, password);
    const encrypted = await sender.encryptSigned(
      textEntity(body),
      "egress@example.test",
      ["egress@example.test"],
    );
    const signingKey = await pgp.decryptKey({
      privateKey: await pgp.readPrivateKey({
        armoredKey: await sender.exportPrivate(generated.fingerprint),
      }),
      passphrase: password,
      config: cryptoConfig,
    });
    const signedData = textEntity(body);
    const detachedSignature = await pgp.sign({
      message: await pgp.createMessage({ binary: signedData }),
      signingKeys: signingKey,
      detached: true,
      format: "armored",
      config: cryptoConfig,
    });
    const signedRaw = new TextEncoder().encode(
      `Content-Type: multipart/signed; protocol="application/pgp-signature"; boundary="reader-detached"\r\n\r\n--reader-detached\r\n${new TextDecoder().decode(signedData)}\r\n--reader-detached\r\nContent-Type: application/pgp-signature\r\n\r\n${detachedSignature.trimEnd()}\r\n--reader-detached--\r\n`,
    );
    await sender.close();
    const raw = new TextEncoder().encode(encryptedEntity(encrypted));
    elements.clear();
    listeners.clear();
    element("secure-mail").dataset = {
      userId: id,
      sessionExpires: String(Date.now() + 60_000),
      read: "true",
      rawUrl: "/api/mail/raw?account=SYNTHETIC_ACCOUNT",
      sender: "egress@example.test",
    };
    let reads = 0;
    globalThis.fetch = async (url, request) => {
      reads++;
      assert.equal(url, "/api/mail/raw?account=SYNTHETIC_ACCOUNT");
      assert.equal(request.method, undefined);
      assert.equal(request.body, undefined);
      return { ok: true, arrayBuffer: async () => raw.buffer };
    };
    await import(
      "../../internal/webui/assets/crypto/mail-ui.mjs?reader-fixture"
    );
    element("mail-passphrase").value = password;
    await listeners.get("mail-unlock:click")();
    await listeners.get("mail-decrypt:click")();
    assert.equal(reads, 1);
    assert.equal(element("decrypted-text").textContent, body);
    assert.equal(element("decrypted-content").hidden, false);
    assert.match(
      element("mail-signature").textContent,
      /verified sender fingerprint/,
    );
    listeners.get("mail-lock:click")();
    assert.equal(element("decrypted-text").textContent, "");
    assert.equal(element("decrypted-content").hidden, true);
    assert.equal(reads, 1);
    // The ordinary signed reader verifies using public contacts only: no unlock
    // and no network request other than the authorized original MIME fetch.
    elements.clear();
    listeners.clear();
    reads = 0;
    element("secure-mail").dataset = {
      userId: id,
      sessionExpires: String(Date.now() + 60000),
      read: "true",
      ordinary: "true",
      rawUrl: "/api/messages/raw?reference=SYNTHETIC_SIGNED",
      sender: "egress@example.test",
    };
    globalThis.fetch = async (url, request) => {
      reads++;
      assert.equal(url, "/api/messages/raw?reference=SYNTHETIC_SIGNED");
      assert.equal(request.method, undefined);
      assert.equal(request.body, undefined);
      return { ok: true, arrayBuffer: async () => signedRaw.buffer };
    };
    await import(
      "../../internal/webui/assets/crypto/mail-ui.mjs?signed-reader-fixture"
    );
    // Ordinary mail opens directly in text mode; no extra Read-as-text click.

    assert.equal(reads, 1);
    assert.equal(element("decrypted-text").textContent, body);
    assert.match(
      element("mail-signature").textContent,
      /verified sender fingerprint/,
    );
    assert.equal(element("mail-passphrase").value, "");
    listeners.get("mail-lock:click")();
    assert.equal(element("decrypted-text").textContent, "");
    // A draft database failure after the key UI is installed must not leave an
    // operational unlock handler without its auto-lock lifecycle.
    elements.clear();
    listeners.clear();
    element("secure-mail").dataset = {
      userId: id,
      sessionExpires: String(Date.now() + 60_000),
      read: "false",
      account: "SYNTHETIC_ACCOUNT",
      from: "egress@example.test",
      requestId: "SYNTHETIC_ENCRYPTED_REQUEST_12345",
      replyFolder: "",
      replyValidity: "0",
      replyUid: "0",
    };
    const originalList = LocalDrafts.prototype.list;
    LocalDrafts.prototype.list = async () => {
      throw new Error("Synthetic draft storage failure");
    };
    try {
      await import(
        "../../internal/webui/assets/crypto/mail-ui.mjs?init-failure-fixture"
      );
      assert.equal(element("mail-unlock").disabled, true);
      assert.equal(element("mail-passphrase").disabled, true);
      await listeners.get("mail-unlock:click")();
      assert.equal(element("secure-compose-fields").disabled, true);
      assert.equal(reads, 1);
    } finally {
      LocalDrafts.prototype.list = originalList;
    }
  } finally {
    Object.assign(globalThis, original);
  }
});
