# Changelog

## Product consolidation (unreleased)

- Authenticated Space dialog at `/`, compact author runs, own replies aligned separately, original cached thread preview and original-target replies.
- Removed legacy Classic/Conversation/Calendar frontend and unauthenticated UI demo; old bookmarks redirect safely.
- Private Space/label management with explicit removal; assignment controls load on demand.
- Stateful personal Compose by default, healthcheck/graceful stop, operator password recovery/session revocation and complete server setup/backup/restore/upgrade runbook.
- Verified isolated HTTPS + TLS IMAP/SMTP deployment, restart persistence and fresh-volume recovery. Personal testing scope; full 1.0 gates remain open in RELEASE_READINESS.md.


## Unreleased — verified mail account setup

- Owner-scoped settings and connection checks for IMAP/SMTP, verified implicit TLS/required STARTTLS.
- Default-deny exact endpoint policy, explicit LAN approvals, DNS pinning, cancellation and bounded checks.
- Provider usernames/passwords encrypted with AES-256-GCM outside user login storage; durable keyring and offline rotation.
- Forward migration v2, encrypted row binding, no password echo/serialization, no partial accounts on failed probes.
- make dev-mail: optional loopback-only seeded TLS IMAP/SMTP fixture with restart-safe UIDVALIDITY.
- Synchronization, sending and OpenPGP remain separate next stages.

## Identity foundation (unreleased)

- Opt-in personal mode: provisioned local users, Argon2id, SQLite WAL and versioned migration.
- Opaque digested sessions, secure cookies, fixed expiry, login rotation and single/all-device logout.
- Exact Origin and synchronizer CSRF protection; bounded per-IP throttling and login concurrency.
- Owner-scoped account metadata and synthetic-data separation, persistent Docker overlay, offline restore tests.
- Mail setup was completed in MS-002; synchronization/sending and OpenPGP remain separate next stages.

## Conversation-first UI revision (unreleased)

- Replaced React/Mantine with Go html/template, HTMX and Tailwind.
- Channel navigation, conversation stream, side thread, classic view and progressive navigation.
- Seven deterministic conversations, seventeen synthetic messages, tags and fixture attachment metadata.
- Local bundled assets, embedded standalone UI, stricter CSP and disabled HTMX history snapshots.
- Makefile for dev/assets/build/seed/check/Docker, with matching container/release builds.

## 0.0.1-alpha.1 foundation (unreleased historical scaffold)

- Go modular-monolith skeleton, read-only fictional API and privacy export policy.
- Go templates + HTMX + Tailwind demo: conversation view, classic inbox and encrypted placeholder.
- AGPL-3.0-only, committed agent/contributor guidelines, architecture, threat model, five ADRs and 17 issue specifications.
- Roadmap includes OpenPGP/MIME, users/team/thread sharing/tags, opt-in modules and YunoHost packaging.
- Dockerfile/Compose and GitHub CI/release definitions with assets, checksums, GHCR, SBOM and provenance.

No real accounts, authentication, IMAP/SMTP, storage or cryptographic operations yet. No release published.

- Read-only real IMAP synchronization, durable UID generations/checkpoints, background polling and personal conversation/classic views. Explicit recent-mail/MIME/cache bounds; no server decryption or remote flag mutations. ADR 0009.

- Ordinary SMTP compose/reply with explicit plaintext choice, durable single-attempt outbox, Bcc-safe CRLF MIME, accepted/rejected/unknown outcomes and independent Sent copy. No automatic resend. ADR 0010.

- Responsive independent pane scrolling, System/Light/Dark, cached-mail Calendar with IANA timezone, functional profile/organization management and repeatable real-service synthetic sandbox. Spaces contract separates matching from sharing. ADR 0011.
