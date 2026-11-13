# Release readiness

Mailspace is deployable for controlled personal testing, not yet a completed 1.0 or independently audited secure mail client. No release/tag/image has been published by this work.

## Working scope

- One root Space dialog, per-message many-to-many private Spaces/labels, manual triage, People/date/metadata filters, original cached thread preview and original-target replies.
- Provisioned users, profile/timezone, organization membership, owner-scoped accounts and sources, CSRF/session controls, operator password recovery and revocation.
- Verified TLS IMAP/SMTP, persistent bounded cache, encrypted provider credentials, durable single-attempt outbox with honest certainty/Sent-copy states.
- Client local keys/trust, signed encrypted OpenPGP/MIME alpha, encrypted local drafts/attachments, text-first MIME and isolated explicit HTML preview.
- Non-root stateful Compose, no mandatory model/index/telemetry, local source/build/release workflow definitions.

## Remaining 1.0 gates

1. Complete the OpenPGP compatibility matrix: detached multipart/signed, additional binary/legacy variants, secure quoting and real Thunderbird acceptance. GnuPG round-trips alone do not close this gate.
2. Complete broader provider/charset/large-mail/history/reconnect acceptance and operational soak tests. Current synchronization is a recent bounded read-only cache, not full archive synchronization or remote seen/archive/delete support.
3. Implement team-source/sharing ACLs before advertising a team mailbox; organizations currently do not grant private-mail access. The optional decision model is not connected. Manual Spaces work without it.
4. Security/accessibility review, release CI execution on GitHub and an explicit reviewed prerelease. Local checks/container smoke do not prove a published multiarch image.

OAuth-only login, PostgreSQL, attachment Drive, full-text/semantic search, external AI/JEV providers and YunoHost remain optional roadmap work. No disabled/fake provider controls should be presented as connected features. Browser keys/local drafts require their own export/recovery procedure.

Deployment procedure and limits: SERVER_SETUP.md. Record actual validation in VALIDATION.md; do not convert open gates into passed claims.
