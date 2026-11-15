# 1.0 completion plan

This is an implementation checklist, not a release declaration. RELEASE_READINESS.md remains the source of truth for passed gates. Each slice needs behavior/security tests and a separate reviewable commit. No automatic stable tag or release until these gates are satisfied.

## Ordered slices

1. **MS-006 compatibility:** binary OpenPGP input, MIME transfer encodings, detached multipart/signed verification with original signed octets, safe quoting and compatibility fixtures. Preserve valid/untrusted/invalid/unknown labels; no private key or silent plaintext export. Run GnuPG and actual Thunderbird acceptance separately.
2. **MS-003/MS-018 mail operations:** charset/large-mail and reconnect acceptance, visible incomplete history/storage budgets, older-history pagination and controlled retention. Preserve account generation/UIDVALIDITY and SMTP unknown-result behavior.
3. **MS-009/MS-010 team use:** explicit source grants, organization membership plus source ACL on every read/download/job, revocation and separate send-as. No implicit private-mail access from a Space/model match; encrypted sharing never transfers keys.
4. **Release security and accessibility:** review authorization/session/SSRF/MIME/export boundaries, keyboard/focus/mobile tests and dependency/license/vulnerability checks. Record findings and fixes; do not call an internal review an independent audit.
5. **Operations and packaging:** upgrade/rollback, database plus installation keyring restore, browser export recovery, executed release/multiarch smoke and a clearly labelled prerelease before stable 1.0.

Optional Drive, full-text/semantic indexing, AI compose, PostgreSQL and YunoHost are follow-up modules, not mandatory dependencies or pretend-complete features. JEV/OpenRouter and Litestream remain opt-in, independently tested boundaries. Live paid-provider availability and real external-client acceptance must be recorded honestly, never inferred from mocks.

## Current evidence

Main f2266a2 has passing GitHub CI, including crypto tests, Go race tests, Docker build and continuous SQLite write/restore drill. 1.0 compatibility/team/security gates remain open. The tested readiness work is merged into main. Work continues directly on main in tested, bounded commits; no pull request is required for owner-directed work. Each completed slice updates this record and RELEASE_READINESS.md before publication.

### MS-006 first compatibility slice

Binary OpenPGP/MIME input is implemented locally (ADR-0023), including MIME transfer-encoding preservation and authenticated local decrypt. Detached signatures/secure quoting/Thunderbird acceptance remain open; no full crypto or 1.0 completion claim.

### MS-006 detached verification slice

ADR-0024 adds exact-octet multipart/signed verification for ordinary and decrypted-inner MIME in the reader. GnuPG detached output and shipped-reader egress/identity behavior are tested. Detached-only sending, secure quotes, wider legacy acceptance and actual Thunderbird remain open.

### MS-018 initial import policy

Migration 008 implements fresh new connections, durable UID baselines and explicit bounded recent import in account settings. Existing accounts retain their prior behavior. Full archive pagination and native IDLE remain open; no history-complete claim.

### Completed slice: reviewed local rules

Schema 9 adds disabled-by-default exact sender/domain and subject-phrase rules, read-only cached previews, explicit enable and bounded post-sync assignment. Manual membership changes veto automatic rules. Sender suggestions after manual assignment are drafts only. Provider-side spam training/moves, archived-rule replay and unbounded history remain outside this slice. See [RULES.md](RULES.md) and ADR-0027.

### MS-006 local signing foundation

ADR-0028 implements local detached signing and exact-byte multipart/signed composition, with application → GnuPG verification and tamper rejection. No signed-only submission endpoint or visible send control is connected by this slice. Signing-only send UX, secure quoting and actual Thunderbird acceptance remain open.

### Spaces workflow slice (ADR-0029)

Thread drawer/original-target replies, immediate Space chips, latest overall Jev score, bounded explicit preview or saved-mode command, 50/page People, retired label UI, 16-color swatches, local archive/restore and attachment downloads/text/PNG previews are implemented. Schema 10 is additive; upgrade requires the usual pre-upgrade SQLite/keyring backup. No remote archive move, general document preview, team ACL or stable 1.0 claim follows.

### Completed personal workflow and next gate (45bd0f1)

AND/OR filters and rule drafts, worker-backed memory sessions, inline local decrypt, optional per-tab autopreview, user-defined sidebar/header labels, inert Markdown text and the mobile view switcher are implemented and pushed. Both recent feature commits passed GitHub CI. A fresh read-only non-root container smoke also passed after restart. Detailed evidence and remaining browser/external-client limits are recorded in VALIDATION.md and RELEASE_READINESS.md.

The next implementation slice is an explicit signed-only send contract and action, preserving original reply references, single-attempt SMTP outcomes, explicit plaintext consent and the encrypted-parent downgrade block. Do not close MS-006 merely because local detached signing or GnuPG tests already pass. Team ACL and operational/compatibility gates remain separate.


### MS-006 authored signed transport foundation

ADR-0037 connects a dedicated authored-only signed MIME endpoint to durable single-attempt SMTP/Sent transport. Application-generated Unicode and binary attachment octets are preserved and verified with GnuPG after delivery through the synthetic provider. Consent/provenance, structural MIME, ownership/CSRF, encrypted-original refusal, stale source and unchanged legacy nonce hashes are covered. Next is the visible full-composer action and client egress acceptance; Thunderbird and secure quoting remain open.


### MS-006 full-composer signed-only action

The full composer now exposes Sign only (unencrypted) with separate explicit consent and a truthful send button. Recipient keys are optional for that mode; verified sender/signing-key checks remain mandatory. Encrypted-source downgrade is disabled in the UI and rejected by the backend. Draft load, lock/hide and sent cleanup reset to encryption and revoke prior plaintext consent. HTTP reply-mode changes retain the selected original references. Client egress and isolated desktop/phone/short-window acceptance cover Unicode/binary attachments and single-attempt ambiguous outcomes. Signed-only implementation is complete for authored full-composer mail; real Thunderbird, secure quoting, broader compatibility/team/security/operations gates remain open.


### MS-006 explicit local quote implementation

ADR-0038 implements exact-source bounded quotes in the full browser composer, without generic decrypted export. Decrypted quotes enforce encryption across local draft v2 save/load; v1 is read conservatively. Tests cover signature states, inert HTML-only conversion, no attachment copying, stale/missing sources, pending-lock cleanup, ciphertext-only egress/persistence and foreign-signed draft injection refusal. The owner requested coding to continue and will perform the manual responsive/external-client acceptance later; those checks remain open, not passed.


### Added owner requirements before 1.0

Complete the remaining recipient/People suggestion UI feedback. Authenticator App 2FA (MS-021/ADR-0040) is implemented with integrated session gating, Security enrollment/local QR, recovery, encrypted seeds, replay/rate/ownership guards and operator reset. Manual real-app/browser acceptance remains open.
