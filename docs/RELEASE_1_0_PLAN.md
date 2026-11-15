# 1.0 completion plan

This is an implementation checklist, not a release declaration. RELEASE_READINESS.md remains the source of truth for passed gates. Each slice needs behavior/security tests and a separate reviewable commit. No automatic stable tag or release until these gates are satisfied.

## Ordered slices

1. **MS-006 compatibility:** binary OpenPGP input, MIME transfer encodings, detached multipart/signed verification with original signed octets, safe quoting and compatibility fixtures. Preserve valid/untrusted/invalid/unknown labels; no private key or silent plaintext export. Run GnuPG and actual Thunderbird acceptance separately.
2. **MS-003/MS-018 mail operations:** charset/large-mail and reconnect acceptance, visible incomplete history/storage budgets, older-history pagination and controlled retention. Preserve account generation/UIDVALIDITY and SMTP unknown-result behavior.
3. **MS-009/MS-010 team use:** ADR-0045/schema 14 deliver the deny-by-default source-grant foundation with immediate revocation and membership-removal checks. Remaining: enforce it on every read/download/job, owner-only management UI, `can_download`, per-user unread/tags/assignment and separate send-as. No implicit private-mail access from a Space/model match; encrypted sharing never transfers keys.
4. **Release security and accessibility:** review authorization/session/SSRF/MIME/export boundaries, keyboard/focus/mobile tests and dependency/license/vulnerability checks. Record findings and fixes; do not call an internal review an independent audit.
5. **Operations and packaging:** upgrade/rollback, database plus installation keyring restore, browser export recovery, executed release/multiarch smoke and a clearly labelled prerelease before stable 1.0.

Optional Drive, full-text/semantic indexing and YunoHost are follow-up modules, not mandatory dependencies or pretend-complete features. PostgreSQL and AI compose are removed from the plan by owner direction. JEV/OpenRouter and Litestream remain opt-in, independently tested boundaries. Live paid-provider availability and real external-client acceptance must be recorded honestly, never inferred from mocks.

## Historical evidence (current resume order: NEXT_SESSION.md)

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

That snapshot preceded the signed-only send contract/action now implemented under ADR-0037, preserving original reply references, single-attempt SMTP outcomes, explicit plaintext consent and the encrypted-parent downgrade block. Current resume priorities are recorded in NEXT_SESSION.md. Do not close MS-006 merely because local detached signing or GnuPG tests already pass. Team ACL and operational/compatibility gates remain separate.


### MS-006 authored signed transport foundation

ADR-0037 connects a dedicated authored-only signed MIME endpoint to durable single-attempt SMTP/Sent transport. Application-generated Unicode and binary attachment octets are preserved and verified with GnuPG after delivery through the synthetic provider. Consent/provenance, structural MIME, ownership/CSRF, encrypted-original refusal, stale source and unchanged legacy nonce hashes are covered. Next is the visible full-composer action and client egress acceptance; Thunderbird and secure quoting remain open.


### MS-006 full-composer signed-only action

The full composer now exposes Sign only (unencrypted) with separate explicit consent and a truthful send button. Recipient keys are optional for that mode; verified sender/signing-key checks remain mandatory. Encrypted-source downgrade is disabled in the UI and rejected by the backend. Draft load, lock/hide and sent cleanup reset to encryption and revoke prior plaintext consent. HTTP reply-mode changes retain the selected original references. Client egress and isolated desktop/phone/short-window acceptance cover Unicode/binary attachments and single-attempt ambiguous outcomes. Signed-only implementation is complete for authored full-composer mail; real Thunderbird, secure quoting, broader compatibility/team/security/operations gates remain open.


### MS-006 explicit local quote implementation

ADR-0038 implements exact-source bounded quotes in the full browser composer, without generic decrypted export. Decrypted quotes enforce encryption across local draft v2 save/load; v1 is read conservatively. Tests cover signature states, inert HTML-only conversion, no attachment copying, stale/missing sources, pending-lock cleanup, ciphertext-only egress/persistence and foreign-signed draft injection refusal. The owner requested coding to continue and will perform the manual responsive/external-client acceptance later; those checks remain open, not passed.


### Added owner requirements before 1.0

Inline multi-recipient To/Cc/Bcc composition is implemented for native and encrypted sending. Complete recipient metadata display and People suggestion reliability. Authenticator App 2FA (MS-021/ADR-0040) is implemented with integrated session gating, Security enrollment/local QR, recovery, encrypted seeds, replay/rate/ownership guards and operator reset. Manual real-app/browser acceptance remains open.


### MS-022 cached timeline paging and next session

ADR-0041 implements the latest 20 matching cached messages, upward 20-message loading, stable scoped cursors, native fallback and retained viewport/body/composer. This closes the requested UI pagination slice, not provider archive synchronization/retention. The owner requested a stop after commit/push of this slice. Resume with the Spaces popover/membership presentation, then automatic/manual provider read/unread, then recipient metadata and People suggestions. NEXT_SESSION.md records that order and the remaining 1.0 acceptance gates.


### MS-022 compact Space picker and visible badges

ADR-0042 replaces the horizontal list of every Space with the shared compact native popover already used for labels. Assigned Space badges stay visible in All activity/Unread/Unsorted; inside one Space only the other assigned Spaces are badged, while every authorized Space remains an independent toggle with current owner/source checks and explicit human overrides. The membership POST now replaces only the picker and refreshes sidebar counts out of band, so mounted bodies, local decrypted content and drafts survive. Next in MS-022: automatic-after-open plus manual provider read/unread with an explicit flag-write capability/ADR, and complete To/Cc/Bcc metadata with reliable People suggestions.


### MS-022 message header identity line

Each started run shows one identity line across the timeline, Spaces, filters and the original-thread drawer: received mail keeps the sender name with the sender address beneath it, sent mail shows `You → recipient` with the recipient summary, and the owning account appears once as its bare address (display name only in the tooltip). Cache previews keep `To` addresses, so recipient display names are not invented and hidden Bcc recipients stay absent. Remaining in MS-022: automatic-after-open plus manual provider read/unread with an explicit flag-write capability/ADR, complete To/Cc/Bcc metadata and reliable People suggestions.


### MS-022 provider-synchronized read state

ADR-0043 adds the only provider write outside sending: a bounded `\Seen` capability with current owner/account revision and UIDVALIDITY checks, server-confirmed flags and a local cache update only after confirmation. Opening the full reader (ordinary on load, encrypted after local decrypt) or an inline decrypted preview marks the message read; the row also has a manual read/unread toggle. Fetching a timeline never writes, outbox rows are excluded, and writes use a dedicated rate/concurrency bound instead of the send budget. `MAILSPACE_IMAP_FLAG_WRITES=0` disables the capability. Remaining MS-022 scope: complete To/Cc/Bcc metadata and reliable People suggestions.


### MS-022 cached recipient names, To/Cc and two-way People

ADR-0044 adds display names and Cc to the bounded cached preview (additive, `RecipientsVersion`), upgraded lazily by the existing header-only, condition-checked repair with no body read, remote request or decryption. Sent mail shows a cached recipient name with the address beneath; received mail shows cached Cc; the full reader shows a To/Cc block. A hidden Bcc is never invented and outgoing Bcc remains envelope-only. The People directory and datalist now cover cached senders plus cached To/Cc recipients, with the client sending only the typed fragment and discarding stale responses. PostgreSQL and AI compose are removed from the plan; SQLite is the only supported storage.
