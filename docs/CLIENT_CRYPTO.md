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

## Detached signing foundation

`LocalVault.signDetached` snapshots its input, revalidates the explicitly bound sender/signing key and rejects lock or binding changes. `signedEntity` preserves canonical seven-bit MIME bytes under multipart/signed with checked SHA-512 signatures. ADR-0028 introduced this local foundation. The explicit authored transport and composer action are now defined in ADR-0037 below; the helper alone never grants decrypted-content export or encrypted-parent downgrade permission.

## Memory-only session and inline preview (ADR-0032)

An available SharedWorker holds the unlocked key in memory across same-origin page views. It checks the authenticated session before and after private operations and locks after five minutes without real activity, session expiry or explicit Lock. No passphrase or unlocked key is persisted. Dedicated-worker and page-local fallbacks remain available; these may ask again after navigation. Browser worker lifetime is not guaranteed. Explicit Lock applies across connected views; closing or hiding a view clears its decrypted DOM and attachments without unnecessarily locking the shared session. This replaces the earlier controller-wide lock-on-every-navigation behavior.

Encrypted timeline messages decrypt inline after explicit Preview locally. Optional autopreview is off by default and only scans visible encrypted messages with an already unlocked key. The preference is held in tab memory, resets on a full reload, and grants no classifier/export permission. Explicit authored signing-only sending is available in the full composer under ADR-0037; inline replies still use ordinary or encrypted sending.

Ordinary and decrypted text presentation follows ADR-0034: fixed DOM from bounded Markdown tokens, folded quotes, no generated HTML/resources/navigation. It does not change original MIME or signature bytes.


Unlock feedback is visible before asynchronous worker/key metadata loading. A failed worker load reports an unavailable session immediately; subsequent calls do not wait on a dead worker. Metadata RPCs time out after eight seconds while cryptographic operations retain their separate longer limit. Failed authentication or transport never silently changes vault mode, sends plaintext, or retries a send. Closing the dialog invalidates a pending display update.


If a SharedWorker's module fails during initial loading, an argument-free list request may recover once in a fresh dedicated worker under ADR-0032. That worker performs the same authentication checks and starts locked. Private operations, login refusals and timeouts are never replayed. Dedicated mode is tab-local; a reload may ask for the passphrase again.


## Explicit authored signed-only mail (ADR-0037)

The full composer defaults to Sign and encrypt. Sign only (unencrypted) reveals a separate, initially unchecked consent explaining that authored text, attachments and filenames will be stored/sent readably. The action says Sign and send unencrypted. Recipient keys are not needed for signing only; the sender fingerprint/signing-key checks remain mandatory. An encrypted original disables this option and is also rejected at the server boundary. Switching from ordinary reply composition preserves the selected source/account/UIDVALIDITY instead of starting an unrelated message.

The body, attachments, keys and passphrase remain unnamed inside hx-disable, with form-action none. Only the explicitly selected JSON contract is submitted. No decrypted-reader selection, decrypted original, automatic quote, generic body field or fallback enters signed submission. Ordinary cached quotes may be included only after the separate signing-only readable-content consent. Own locally encrypted authored drafts remain encrypted at rest; loading one resets protection to encryption and clears any previous plaintext consent. Lock, hide and successful send also clear consent and return to encryption. Unknown sends stay submitted and require checking Outbox; no automatic retry or downgrade occurs.


## Explicit local original quotes (ADR-0038)

In the full reply composer, Quote original locally fetches only the selected account/folder/UID/UIDVALIDITY through the existing authorized raw endpoint. No automatic quoting or subject-based source selection occurs. MIME decryption and signature checking remain local; only up to 32 KiB of inert text is prefixed with `> ` and placed in the unnamed crypto textarea. Original attachments are not copied. Invalid signatures and oversized/empty originals are refused; unsigned/unknown/untrusted/verified states are displayed honestly. A decrypted quote makes encryption mandatory and revokes signing-only consent, including after explicit draft save/load. Lock or navigation prevents pending work from restoring text.

New local ciphertext drafts use authenticated payload version 2 for this protection flag. Version 1 stays readable; legacy reply drafts conservatively require encryption. Saved fingerprints must match the unlocked personal key, so third-party signatures cannot impersonate a self-draft via edited metadata. Old builds cannot load version-2 drafts: preserve browser storage and use a compatible build for recovery. No server schema or plaintext draft storage is added. Manual responsive/external-client acceptance is left to the owner as requested.


Inline recipients (MS-020 follow-up, existing ADR-0013/0016 boundary): visible To/Cc/Bcc each accept bounded comma-separated lists. The client validates all groups before key work; every entered address participates in trust validation and signed encryption. The routing/account/reference snapshot is fixed before the asynchronous encryption operation and is the same explicit JSON metadata submitted with ciphertext. Empty To is supported when Cc or Bcc contains a recipient. Header/control injection and excessive combined counts fail without egress or plaintext fallback. No new endpoint, data format, storage, dependency or decrypted export is introduced. Ordinary native/HTMX submits use the existing owner-scoped draft contract. Composer fields have scoped labels/datalists; its own bounded scroll preserves the single application viewport in short windows.
