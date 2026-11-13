# Browser crypto boundary after HTMX migration

Go renders app chrome and ordinary mail. HTMX must never post decrypted mail, private keys or passphrases back as form data. This remains a hard boundary even though the rest of the UI is server-rendered.

M2 introduces a small browser JavaScript module owning the encrypted local vault, unlock/lock, encrypt/decrypt/sign/verify and MIME preparation. It renders decrypted content into an isolated client-owned DOM area with `hx-disable`; no HTMX serialization, history caching, event payloads, attributes or server templates may contain that plaintext. Explicit form scoping and network/DOM regression tests must prove it. Server-rendered normal mail continues to use Go html/template escaping.

HTMX config: allowEval=false, allowScriptTags=false, selfRequestsOnly=true, historyCacheSize=0, hx-history=false; no inline event handlers. Go CSP no longer needs unsafe-inline for styles or scripts. Local assets only, with normal full-page navigation as fallback.

Interface sketch (internal design, not public SDK): decryptMime(ciphertext), verifyMime(originalBytes), signMime(canonicalBytes, fingerprint), encryptMime(canonicalBytes, verifiedRecipients), lock(). It is deliberately not implemented by server code. See OPENPGP.md and MS-005/MS-006.

## Implemented local key management (MS-005)

`/keys` now implements the isolated encrypted local vault, generation/import/export, lock and explicit recipient fingerprint trust described in ADR-0012. Its CSP denies network connections and form submission. Session lock signals carry no key material. Back up protected private keys and the separately downloaded revocation certificate before clearing browser storage. Unlock is memory-only; JavaScript cannot guarantee RAM erasure or protect against malicious delivered code/extensions.

Mail encryption/decryption/MIME remains MS-006: `localKeyVault=true` does not imply `openpgp=true`. Existing encrypted mail stays opaque and ordinary compose still requires explicit plaintext consent.

MS-006 integrated alpha is now available at `/secure/compose` and `/secure/read`: signed encrypted text, authorized opaque MIME submission, local decryption and embedded-signature labels. No private-key API or plaintext autosave. The later ADR-0021 adds a dedicated consented selected-excerpt classification API, separate from generic crypto/HTMX flows. Full compatibility/detached-signature/Thunderbird gates remain open; see ADR-0013.

`web/crypto/drafts.mjs` stores only signed ciphertext (20 drafts / 16 MiB) in a separate user namespace; subject/recipients/body/attachment bytes and filenames are inside encryption. Explicit save/load only, no server/IMAP drafts. File input is unnamed and attachments enter the same locally encrypted MIME; ADR-0014.

Text-first HTML handling is defined in ADR-0015. The only approved decrypted HTML sink is DOMPurify-sanitized iframe.srcdoc in an empty sandbox within hx-disable. No general decrypted data attributes, named fields, SSR, HTMX or backend export. Close/lock removes the source document.

## Inline timeline composer (ADR-0018)

Write first, choose Encrypt beside Send. Ordinary replies require the explicit Send unencrypted action. Encrypted mode removes the textarea name, disables HTMX for the form and captures submission; only explicit metadata plus encrypted MIME enters JSON. A compact local key/passphrase dialog has no named private controls. Missing/changed/untrusted recipient keys block encryption and link to Local keys. Unlocking does not send. Encrypted-parent replies cannot switch to ordinary plaintext. Hidden page, lock broadcast, expiry, inactivity, navigation and HTMX replacement lock/dispose the local controller. An uncertain ciphertext POST stays submitted; check Outbox before any new attempt. Attachments and encrypted local drafts remain in the advanced composer in this slice.

## Explicit selected-excerpt exception

See [ADR-0021](adr/0021.md) and [AUTOMATION.md](AUTOMATION.md). After local decryption, a user can approve one exact excerpt for the displayed classifier. Only that excerpt crosses the backend in memory; keys/passphrases/rest of mail stay local. No excerpt/hash/headers persist in trace. Results always require human approval. This does not permit generic decrypted exports, named form fields, HTMX serialization or silent background calls.
