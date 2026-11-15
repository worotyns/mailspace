# Next session — owner handoff, 2026-11-15

Work directly in the existing checkout on main. Preserve changes, use no worktrees/PRs, commit and push each completed working slice to origin/main. Never force-push, publish a release or deploy without separate authorization. The owner requested stopping after the cached-history loading slice and returning to 1.0 work in a later session.

## Delivered in the latest work

- Authenticator App 2FA: encrypted seed storage/recovery, password-gated pending login, Settings enrollment/local QR, first-code activation, code replacement/disable and local operator reset. Empty-password browser submission fixed in 267c805; Security stays inside the modal and refreshes parent session metadata in 3b34ed1. Real app/Safari acceptance is still separate.
- Inline To/Cc/Bcc recipient lists for ordinary and encrypted mail (c58ef15), with trust for every recipient, private Bcc envelope and no encrypted-parent downgrade.
- Compact native label popovers and secondary email addresses under contact names (aa7b938).
- ADR-0041: latest-20 root timeline, upward older cached pages, bounded scoped cursors, native fallback and retained viewport/decrypted DOM/composer. This is pagination of the current cache, not an unlimited provider archive. The final targeted checks are recorded in VALIDATION.md; broad test repetition was waived by the owner for this slice.
- ADR-0042: compact Space picker sharing the label popover module. Assigned Space badges are visible in All activity/Unread/Unsorted; inside one Space only the other assigned Spaces are badged, while every authorized Space stays an explicit toggle. The membership response replaces only the picker and refreshes sidebar counts, so mounted bodies, decrypted previews and drafts survive. See VALIDATION.md for evidence; real browser/keyboard/mobile acceptance remains open.
- Message header identity pair (ADR-0033 polish): received mail shows `sender name → account name` with `sender address  account address` beneath; sent mail shows `You → recipient summary` with the account and recipient addresses. Applied to the timeline, Spaces, filters and the original-thread drawer. Recipient display names are not invented because cache previews keep addresses only.
- ADR-0044 cached recipient names, To/Cc and two-way People: sent mail shows a cached recipient name, received mail shows cached Cc, the reader shows a To/Cc block and suggestions cover people this user wrote to. Hidden Bcc is never invented; outgoing Bcc stays envelope-only. Owner direction removed PostgreSQL and AI compose from the plan.
- ADR-0043 provider-synchronized read state: a bounded `\Seen` capability with owner/revision/UIDVALIDITY checks and server-confirmed flags, automatic marking on an explicit content open plus a manual toggle, and a dedicated rate/concurrency bound. `MAILSPACE_IMAP_FLAG_WRITES=0` disables it. See VALIDATION.md; real-provider and manual acceptance remain open.

## Next implementation slices, in owner priority order

1. **Team sharing (MS-009/MS-010):** explicit source grants, organization membership plus a source ACL on every read/attachment/job, revocation checks and separate send-as. New ADR with migration/rollback and mandatory two-user denial tests. This is the largest remaining trust-boundary slice; organizations currently grant no private-mail access.
2. **Then, in order:** local full-text/semantic search without a model (MS-012, reworded MS-014), Drive attachments, Litestream opt-in hardening, YunoHost packaging. PostgreSQL and AI compose are removed from the plan.

## Remaining 1.0 gates

- External OpenPGP compatibility and real Thunderbird/secure-quote/manual key/draft recovery acceptance; GnuPG/Chromium fixtures do not prove those clients.
- Broader provider, charset/large MIME, reconnect/soak, history/retention and visible storage-budget acceptance. Current cached paging does not implement full archive sync, native IMAP IDLE or remote archive/delete.
- Team-source/sharing ACLs, revocation and separate send-as. Organization membership and Space rules currently grant no private-mail access.
- Security/accessibility/dependency/license review, Safari/mobile/manual acceptance and real Authenticator pairing. Distinguish internal testing from independent audit.
- Current-commit CI/release/multiarch validation, upgrade/rollback and SQLite + credential-keys + auth-keys restore plus separate browser key/draft recovery. A reviewed prerelease/stable publication remains a separately authorized action.

Optional OAuth-only providers, Drive, full-text/semantic indexing and YunoHost are not mandatory hidden core dependencies. PostgreSQL and AI compose were removed from the plan. Follow RELEASE_READINESS.md and RELEASE_1_0_PLAN.md rather than declaring 1.0 complete from the personal workflow alone.
