# Mailspace

Self-hosted email, organized around your projects and conversations.

[![Checks](https://github.com/worotyns/mailspace/actions/workflows/ci.yml/badge.svg)](https://github.com/worotyns/mailspace/actions/workflows/ci.yml)
[![License: AGPL-3.0-only](https://img.shields.io/badge/license-AGPL--3.0--only-blue.svg)](LICENSE)

Mailspace combines a chat-style interface with your existing IMAP/SMTP provider. Organize individual messages into **Spaces**, follow their original email threads when you need context, and reply without switching back to a traditional inbox.

Built with Go, HTMX, Tailwind and SQLite. Fully open source. No mandatory AI, indexing service or telemetry.

**Early alpha — for self-hosted testing and feedback.** Real mailbox setup, persistent mail cache, ordinary replies and a client OpenPGP/MIME alpha work. The full compatibility/security/team gates for 1.0 remain explicit in [release readiness](docs/RELEASE_READINESS.md). The source can be built with Docker Compose; a published release or registry image is not required.

## The product

- One dialog at `/`, opening incoming Unsorted triage by default (inbox zero when everything has a Space), with chronological messages, compact consecutive-author groups and your replies visually separated. People, labels, metadata search and a small date jump use the same timeline.
- A Space is an area of relevance, not an IMAP folder or an email thread. One message can belong to several Spaces; messages from the same email thread can belong to different Spaces. Manual Unsorted triage works without a model.
- **View email thread** opens the original cached email conversation across the current account's folders/outbox. Reply there or from a Space: the selected original message supplies From/References/In-Reply-To. Subject similarity never invents a thread; missing history and ambiguous Message-IDs are handled conservatively.
- Write replies inline. Explicit Send unencrypted for ordinary mail, or Encrypt and unlock a local key in a compact dialog. No silent encrypted-parent downgrade. Advanced encrypted compose includes local drafts and attachments.
- Native text/plain first; HTML-only mail opens automatically as readable text, without tags or HTML layout. Optional isolated formatted preview blocks external resources.
- [Local rules](docs/RULES.md): reviewed sender/domain/subject matches assign incoming cached mail to Spaces without a model.
- Local browser keys, explicit fingerprint trust, encrypted provider credentials, no telemetry/model/index by default. Organization membership is separate from private mail access.

The local preview uses the same authenticated application with synthetic mail and isolated TLS providers.

![Mailspace Space dialog in light mode, using synthetic mail](docs/images/space-dialog-light.png)

<details>
<summary>Dark mode on mobile</summary>

<img src="docs/images/space-dialog-mobile-dark.png" width="390" alt="Mailspace mobile Space dialog in dark mode, synthetic messages only">

</details>

## Try locally

Development requirements: Go 1.27.4+, Node 24.15+ (or 26), npm.

```sh
git clone https://github.com/worotyns/mailspace.git
cd mailspace
make GOTOOLCHAIN=go1.27.4 dev-sandbox
```

Open http://127.0.0.1:8080/. Login `demo@example.test`; generated password in `data/dev-sandbox/login-password`. This is a loopback-only synthetic IMAP/SMTP installation; it never relays mail. [Preview details](docs/DEV_SANDBOX.md). `make dev` runs the same fixture with templates/assets read from disk. No production/demo passwords are bundled.

```sh
make help
make GOTOOLCHAIN=go1.27.4 check
make GOTOOLCHAIN=go1.27.4 build
```

Build assets before compiling Go. Run `make build` after committing: its source archive represents HEAD, not uncommitted changes. Node is not needed at runtime. The native server defaults to authenticated personal mode and requires an explicit browser-facing origin.

## Deploy on your server

See the [environment variable reference](docs/CONFIGURATION.md) for every setting, its default and examples.

The main Compose settings mean:

| Setting | What it controls |
|---|---|
| `MAILSPACE_MODE=personal` | The authenticated installation mode. `personal` is currently the only supported value; it is not an organization selector, UI preference or AI switch. Multiple provisioned logins can have separate private data, but shared team mail access is not implemented. Leave this unchanged. |
| `MAILSPACE_DB=/app/data/mailspace.db` | SQLite file **inside the container**, persisted in the data volume. This is not a host path. |
| `MAILSPACE_ORIGIN` | Exact browser-facing HTTPS origin, e.g. `https://mail.example.com`, used for secure cookies and request validation. Required, including behind a reverse proxy. |
| `MAILSPACE_MAIL_ENDPOINTS` | Exact comma-separated public IMAP/SMTP destinations, e.g. `imap.example.com:993,smtp.example.com:465`. Empty denies public mail connections. Account credentials are entered in the app. |
| `MAILSPACE_PRIVATE_MAIL_ENDPOINTS` | Separate explicit allowlist for private/LAN/container mail servers. Empty denies private mail connections. |
| `MAILSPACE_MAIL_CA_FILE` | Optional public CA certificate PEM **inside the container** for a private mail CA. Mount the file read-only as well; TLS hostname verification stays enabled. Empty uses system CAs. |
| `MAILSPACE_JEV_PROVIDER` | Optional classifier: empty disables its client and worker; `openrouter` or `local` selects an adapter. Each user must also opt in under Space automation. |
| `MAILSPACE_JEV_MODEL` | Exact model identifier, e.g. `typesafe/jev-1.13` for OpenRouter Jev Decisions; required when classification is configured. There is no default model. |
| `MAILSPACE_JEV_API_TOKEN` | Provider token supplied directly through the environment. Use this **or** `MAILSPACE_JEV_KEY_FILE`, never both. Keep secrets in ignored `.env` or a secret manager. |
| `MAILSPACE_JEV_KEY_FILE` | Alternative token file, readable by the app, mode `0600`; this is a container path, e.g. `/app/data/openrouter-api-key`. It is not an OpenPGP key. |
| `MAILSPACE_JEV_ENDPOINT` | Full compatible chat-completions URL for `local` only. OpenRouter uses its fixed endpoint. HTTPS is required except for numeric loopback HTTP. |

`MAILSPACE_MODE` and `MAILSPACE_DB` are fixed in the supplied Compose file: adding them to `.env` does not override those literals. The other settings are substituted from `.env`. Blank `${NAME:-}` values mean unset/empty; `${NAME:?message}` rejects missing required values. The native binary does not read `.env` automatically.

Follow [SERVER_SETUP.md](docs/SERVER_SETUP.md) for the complete procedure: HTTPS, persistent volume, provisioning users, configuring IMAP/SMTP, local keys, offline backup/restore, upgrades and account recovery.

```sh
cp .env.example .env
# Edit .env: your HTTPS origin and exact provider endpoints.
docker compose config --quiet
docker compose build
docker compose run --rm --no-deps mailspace user add you@example.com
docker compose up -d
```

Compose binds only to localhost and persists `/app/data` in a named volume. Put an HTTPS reverse proxy in front. New mailbox connections start fresh; import the recent window explicitly from account settings if wanted. No default password, public registration or public unauthenticated demo. Do not use `down -v` for normal stops/upgrades. The native binary does not load `.env` automatically.

## Scope and limits

Recent bounded read-only IMAP cache, not full archive synchronization or remote flag/delete/archive management. SMTP uses password authentication over verified TLS; OAuth-only providers are not implemented. One app process per SQLite installation. OpenPGP compatibility is alpha, with the Thunderbird/detached-signature matrix still open. Browser vaults/drafts need separate exports; server backup does not recover them.

Optional [Space automation](docs/AUTOMATION.md) uses local/OpenRouter proposals behind a deterministic JEV gate. It defaults to Off and supports dry-run, review, auto-assignment and replay. [Continuous SQLite backup](docs/BACKUPS.md) uses an explicit Litestream overlay and is also off by default.

Team sharing, attachment Drive, full-text/semantic search, PostgreSQL and YunoHost remain roadmap work. None is mandatory or displayed as connected. Encrypted content cannot silently enter backend indexing, AI, webhook or trace. Modules are reviewed built-in contributions: fork → PR → review → merge; no public plugin loader/API.

## Contributing and source

[Agent guidelines](AGENTS.md) · [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) · [Roadmap](docs/ROADMAP.md) · [Backlog](docs/BACKLOG.md) · [Architecture](docs/ARCHITECTURE.md) · [Threat model](docs/THREAT_MODEL.md) · [ADRs](docs/adr/README.md) · [Validation](docs/VALIDATION.md).

Own code is **AGPL-3.0-only**, with no closed paid core. Dependencies retain their licenses/notices. The running app serves corresponding build sources at `/source.tar.gz`; Docker/release builds include them. Release workflows are prepared, but publication is a separate reviewed action. Report reproducible bugs and usability feedback through [GitHub Issues](https://github.com/worotyns/mailspace/issues). Keep real mail, credentials and private keys out of reports. See [SECURITY.md](SECURITY.md) for vulnerability reporting.
