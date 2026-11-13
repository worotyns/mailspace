# Full local preview with synthetic mail

```sh
make dev-sandbox
```

This builds local assets and starts the real Go application, a verified-TLS development IMAP/SMTP fixture, two local users and a Northstar studio organization. HTTP binds to 127.0.0.1:8080; the fixture uses 127.0.0.1:21993/21465. Stop an existing listener first. No real provider, AI, index or external mail delivery is used.

Sign in as demo@example.test using the random password in data/dev-sandbox/login-password. The other test login is member@example.test, with its independent password in data/dev-sandbox/member-password. These files are 0600, generated once and never printed into logs. App login passwords are separate from the provider password in provider/password. Never reuse them for actual mail. This command is not the production container entrypoint.

The preview supports real native account forms, read-only synchronization, ordinary compose/reply/outbox, Sent APPEND, the root Space dialog, original cached email-thread preview, profile, organizations and appearance. Mail is synthetic .test data. Personal-mode isolation and TLS remain enabled; only the numeric loopback fixture endpoints are approved. Appearance is browser-local; names, timezone, organization and sessions persist in the private app DB.

Restart resets the fixture's in-memory mail and advances UIDVALIDITY. App users, settings, organization and outbox records persist. Historical Sent copies in the fixture may disappear after restart; old accepted outbox entries are never resent. Losing an existing app login file causes startup refusal rather than silent password reset. To create a fresh disposable sandbox, choose a new state directory with go run ./cmd/devsandbox -data-dir PATH. HTTP and fixture bind flags require numeric loopback.

make dev runs the same authenticated sandbox with templates/assets read from disk. The old standalone unauthenticated UI is removed. Real private provider setup uses cmd/mailspace personal mode as documented in IDENTITY.md and MAIL_ACCOUNTS.md. Do not publish the development sandbox or treat synthetic mail as private production data.
