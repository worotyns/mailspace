// SPDX-License-Identifier: AGPL-3.0-only
import "fake-indexeddb/auto";
import test from "node:test";
import assert from "node:assert/strict";
import { LocalVault } from "../../internal/webui/assets/crypto/vault.mjs";
import { VaultStore } from "../../internal/webui/assets/crypto/store.mjs";
import { originalQuote } from "../../internal/webui/assets/crypto/quote.mjs";
import {
  composeEntity,
  encryptedEntity,
  signedEntity,
} from "../../internal/webui/assets/crypto/mime.mjs";
const encode = (value) => new TextEncoder().encode(value);
const envelope = (body) =>
  encode("From: quote@example.test\r\nTo: quote@example.test\r\n" + body);

test("quotes preserve inert Unicode text, signature states and encrypted provenance without attachments or network", async () => {
  const previous = globalThis.fetch;
  globalThis.fetch = () => {
    throw new Error("quote helper attempted network access");
  };
  const signer = new LocalVault(new VaultStore("SYNTHETIC_QUOTE_SIGNER_USER"));
  const reader = new LocalVault(new VaultStore("SYNTHETIC_QUOTE_READER_USER"));
  try {
    const key = await signer.generate(
      "Quote fixture",
      "quote@example.test",
      "Synthetic quoted message passphrase",
    );
    await signer.trust("quote@example.test", key.fingerprint, key.fingerprint);
    await signer.unlock(key.fingerprint, "Synthetic quoted message passphrase");
    const entity = composeEntity("Zażółć quoted text\n> Prior quote", [
      {
        name: "PRIVATE_ATTACHMENT.bin",
        data: encode("PRIVATE_ATTACHMENT_BYTES"),
      },
    ]);
    const signature = await signer.signDetached(entity, "quote@example.test");
    const raw = envelope(signedEntity(signature.data, signature.signature));
    let quote = await originalQuote(raw, reader);
    assert.equal(quote.text, "> Zażółć quoted text\n> > Prior quote");
    assert.equal(quote.encryptionRequired, false);
    assert.equal(quote.provenance, "ordinary-mail");
    assert.match(quote.signatureState, /Unknown signing key/);
    assert.equal(JSON.stringify(quote).includes("PRIVATE_ATTACHMENT"), false);
    await reader.importPublic(key.publicKey);
    quote = await originalQuote(raw, reader);
    assert.match(quote.signatureState, /identity is not trusted/);
    await reader.trust("quote@example.test", key.fingerprint, key.fingerprint);
    quote = await originalQuote(raw, reader);
    assert.match(quote.signatureState, /verified sender fingerprint/);
    const encrypted = await signer.encryptSigned(entity, "quote@example.test", [
      "quote@example.test",
    ]);
    quote = await originalQuote(envelope(encryptedEntity(encrypted)), signer);
    assert.equal(quote.encryptionRequired, true);
    assert.equal(quote.provenance, "client-decrypted");
    assert.equal(quote.text, "> Zażółć quoted text\n> > Prior quote");
    const changed = entity.slice();
    changed[changed.length - 5] ^= 1;
    await assert.rejects(
      originalQuote(
        envelope(signedEntity(changed, signature.signature)),
        reader,
      ),
      { code: "invalid-signature" },
    );
    const html = await originalQuote(
      envelope(
        'Content-Type: text/html; charset=utf-8\r\n\r\n<p>Inert quote</p><img src="https://forbidden.example.test/track"><script>alert(1)</script>',
      ),
      reader,
    );
    assert.match(html.text, /Inert quote/);
    assert.equal(html.text.includes("<img"), false);
    assert.equal(html.text.includes("<script"), false);
    assert.match(html.signatureState, /Unsigned original/);
    const plain = await originalQuote(
      envelope("Content-Type: text/plain; charset=utf-8\r\n\r\nOrdinary text"),
      reader,
    );
    assert.equal(plain.text, "> Ordinary text\n> ");
    await assert.rejects(
      originalQuote(
        envelope(
          "Content-Type: text/plain\r\n\r\n" + "x".repeat((32 << 10) + 1),
        ),
        reader,
      ),
      { code: "quote-limit" },
    );
    await assert.rejects(
      originalQuote(envelope("Content-Type: text/plain\r\n\r\n"), reader),
      { code: "quote-empty" },
    );
    signer.lock();
    await assert.rejects(
      originalQuote(envelope(encryptedEntity(encrypted)), signer),
      { code: "locked" },
    );
  } finally {
    await signer.close();
    await reader.close();
    globalThis.fetch = previous;
  }
});
