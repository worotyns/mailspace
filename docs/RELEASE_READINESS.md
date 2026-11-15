# Release readiness

Mailspace is deployable for controlled personal testing, not yet a completed 1.0 or independently audited secure mail client. No release/tag/image has been published by this work.

## Working scope

- One root Space dialog, per-message many-to-many private Spaces/labels, manual triage, People/date/metadata filters, original cached thread preview and original-target replies.
- Provisioned users, profile/timezone, organization membership, owner-scoped accounts and sources, CSRF/session controls, operator password recovery and revocation.
- Verified TLS IMAP/SMTP, persistent bounded cache, encrypted provider credentials, durable single-attempt outbox with honest certainty/Sent-copy states.
- Client local keys/trust, signed encrypted OpenPGP/MIME alpha, encrypted local drafts/attachments, text-first MIME and isolated explicit HTML preview.
- Non-root stateful Compose, no mandatory model/index/telemetry, local source/build/release workflow definitions.

## Remaining 1.0 gates

1. Complete the OpenPGP compatibility matrix: additional legacy variants and manual secure-quote/real Thunderbird acceptance. The full-composer local quote implementation is covered by ADR-0038. Authored signed-only sending is implemented under ADR-0037, with external-client acceptance still required. GnuPG round-trips alone do not close this gate.
2. Complete broader provider/charset/large-mail/history/reconnect acceptance and operational soak tests. Current synchronization is a recent bounded read-only cache, not full archive synchronization or remote seen/archive/delete support.
3. Implement team-source/sharing ACLs before advertising a team mailbox; organizations currently do not grant private-mail access. Optional JEV classification is connected only after operator configuration and per-user consent; live paid OpenRouter routing is not validated by CI. Manual Spaces work without it.
4. Security/accessibility review, release CI execution on GitHub and an explicit reviewed prerelease. Local checks/container smoke do not prove a published multiarch image.

OAuth-only login, PostgreSQL, attachment Drive, full-text/semantic search, AI compose/summarize and YunoHost remain optional roadmap work. No disabled/fake provider controls should be presented as connected features. Browser keys/local drafts require their own export/recovery procedure.

Deployment procedure and limits: SERVER_SETUP.md. Record actual validation in VALIDATION.md; do not convert open gates into passed claims.

Optional JEV (ADR-0020), consented selected-excerpt classification (ADR-0021) and Litestream overlay (ADR-0022) are testing features, not a completed security audit or hosted-provider availability guarantee. Litestream covers SQLite only; keyring/browser exports remain separate.

1.0 work is tracked in [RELEASE_1_0_PLAN.md](RELEASE_1_0_PLAN.md). Binary OpenPGP/MIME read support is implemented in ADR-0023 and detached signature reading in ADR-0024; authored signed-only sending is now implemented in ADR-0037; secure quoting is implemented in ADR-0038, while its manual acceptance and the real Thunderbird matrix remain open.

The testing branch also implements explicit cached-message Jev previews and an incoming Unsorted queue with an inbox-zero state (ADR-0025), fresh new connections with explicit bounded recent import (ADR-0026), and reviewed local header rules (ADR-0027). HTML-only readers automatically show inert text; formatted preview is optional. Two synthetic live Jev Decisions requests passed under explicit user authorization; this is not a production-mail accuracy or general provider availability claim.

Local detached signing and multipart/signed composition now have an application → GnuPG interoperability test (ADR-0028). The authored-only submission endpoint is implemented in ADR-0037, with exact-octet HTTP/SMTP/Sent/GnuPG verification. The full composer now offers a visible signed-only action with separate readable-content consent; encryption stays the default and encrypted-source downgrade stays blocked. Owner-directed work continues directly on main.

ADR-0029 adds the Spaces-only workflow, thread drawer, bounded People/commands, local archive and explicit attachment viewing. ADR-0030/0033 restore user-defined labels in Settings, sidebar filters and inline message headers; schema 10 preserved their existing records/colors. See WORKFLOW.md for limits and local-versus-provider behavior. These changes do not close the team-sharing or broader 1.0 gates.

## Current testing snapshot: 45bd0f1

The tested personal workflow now includes shared AND/OR filter/rule semantics (ADR-0031), memory-only worker vault sessions and inline local decryption/autopreview (ADR-0032), user-defined sidebar/header labels (ADR-0033), inert Markdown display (ADR-0034), and a mobile title-triggered view switcher. These are implemented slices, not remaining implementation tasks. Label names and colors come from the user's records; no fixed status set is created.

Both fdb5a52 and 45bd0f1 passed GitHub CI, including Docker build and ongoing SQLite backup/restore. Local make check passed 59 client tests, Go vet/race suites and asset/format checks. A fresh production-image smoke verified health, login, bundled Markdown/parser notices, non-root read-only startup and user persistence after restart using disposable synthetic data. Browser acceptance verified inline decrypt/signature/attachment display and SharedWorker reuse after reload; phone 390×844 view switching and independent feed scrolling were checked. Native Safari and real Thunderbird acceptance are still unperformed. See VALIDATION.md for the distinction between these checks and full release acceptance.

Authored signed-only sending is implemented end to end in the full composer (ADR-0037), including explicit consent and exact-byte transport. Secure quoting is implemented under ADR-0038; its manual acceptance and real Thunderbird remain open. Team source/sharing/send-as ACLs, wider compatibility/soak and reviewed prerelease acceptance remain open. No stable tag/release is authorized by passing these slices alone.
