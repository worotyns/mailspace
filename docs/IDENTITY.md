# Personal mode and local users

Authenticated personal mode is the default. The old unauthenticated demo has been removed; make dev-sandbox runs the actual product against isolated synthetic providers. Personal mode adds real users and sessions, with IMAP/SMTP connection setup in MS-002; read-only synchronization is available (MAIL_SYNC.md), ordinary sending/replies are available (SENDING.md). No public registration or outbound signup mail.

## Local development

Build once with `make build`. Provision your own user interactively; password is read without terminal echo:

```sh
./mailspace user add you@example.test
```

The default DB path is `data/mailspace.db`; set `MAILSPACE_DB` to change it. CLI also accepts a password from stdin (for controlled provisioning); never pass passwords as command-line arguments or commit them to environment files. Minimum 12 characters, maximum 1024 UTF-8 bytes.

```sh
MAILSPACE_MODE=personal MAILSPACE_ORIGIN=http://127.0.0.1:8080 MAILSPACE_INSECURE_HTTP=1 ./mailspace
```

The HTTP exception is accepted only for loopback origins. Production uses `MAILSPACE_ORIGIN=https://mail.example.test` behind a trusted TLS reverse proxy, preserving the host and Origin headers. No automatic proxy-header trust. Cookie Secure remains enabled. Existing public demo deployments must not be mistaken for protected personal mode.

## Docker opt-in

```sh
MAILSPACE_ORIGIN=https://mail.example.test docker compose -f compose.yaml -f compose.personal.yaml up --build -d
MAILSPACE_ORIGIN=https://mail.example.test docker compose -f compose.yaml -f compose.personal.yaml run --rm mailspace user add you@example.test
```

The named volume persists `/app/data`; the container still runs as an unprivileged user. TLS/reverse proxy setup belongs to the operator. No user or default password is created automatically. Do not add the personal overlay to public demo builds inadvertently.

## Sessions and permissions

Login requires an exact configured Origin. Session cookie is HttpOnly/SameSite Strict; production is Secure with __Host- prefix. Tokens are random, digested in SQLite, expire after eight hours and can be revoked on one/all devices. Logout additionally requires the session CSRF token. APIs return 401 for anonymous requests, and account access uses owner-scoped queries. Personal mode does not expose the shared demo conversations.

MS-002 adds encrypted provider credentials and verified connection setup; see MAIL_ACCOUNTS.md. Browser OpenPGP vault data remains entirely separate. Profiles/timezones and organization membership are available (SPACES.md); they never grant private-mail access. Operator-only `user password EMAIL` resets the app password and atomically revokes that user’s sessions; `user revoke-sessions EMAIL` revokes sessions without changing the password. Authenticator App 2FA is available per user under Settings → Security. No public password-reset mail flow or open signup is implemented. These commands do not reset provider or OpenPGP passwords.

## Offline backup / rollback

1. Stop all app and provisioning processes that access the database.
2. Copy the entire data directory, including any SQLite WAL/SHM files. Protect the backup at least as carefully as the DB; hash data and metadata are sensitive.
3. To restore, keep the app stopped, restore the entire directory and correct ownership/permissions (directory 0700, DB 0600).
4. Start the binary version matching that backup. A binary refuses a newer schema version. Verify login, account ownership and session revocation using a test restore before relying on the backup.

Do not copy a live SQLite file alone. Migration v1 is forward-only; no destructive down migration is provided. A future online backup command must use SQLite's consistent backup mechanism. Revoke all sessions after restoring an old backup if previously revoked sessions could otherwise reappear.


## Authenticator App 2FA (MS-021, ADR-0040)

Migration 13 adds encrypted TOTP configuration, hashed one-time recovery codes and password-version-bound short-lived challenges. Existing users remain password-only until explicit enrollment. Settings → Security requires your current password to start a ten-minute, session-bound setup. Scan the locally generated QR or enter the setup key into your Authenticator app (TOTP, six digits, 30 seconds, SHA-1), then confirm the first code. No external QR service receives the seed. Save the ten recovery codes shown once on activation; they are never redisplayed. Keep them outside the browser.

After activation, a password creates only a five-minute pending challenge. `/auth/mfa` requires an app code or an explicitly selected unused recovery code before any mail/API/session access. Both steps require the configured Origin; the second step also has its own CSRF token. Codes cannot be reused, including the app step used during activation. Wait for a fresh app code before the next security action. Shared failure limits survive new challenges. Native forms work without JavaScript; local enhancement clears secrets on leaving/locking the page and adapts the code keyboard.

Disabling the factor or replacing recovery codes requires the current password and a valid factor. Enabling, disabling and replacing codes rotate the current session and revoke other sessions/pending sign-ins. Password reset preserves enabled 2FA. A lost app can use a saved recovery code; if neither factor is available, only an operator with local installation access can explicitly reset it:

```sh
./mailspace user reset-2fa you@example.test
# With the configured persistent Compose volume:
docker compose run --rm --no-deps mailspace user reset-2fa you@example.com
```

This removes the factor/recovery set and revokes all sessions and pending sign-ins. It keeps the password, mail provider credentials and browser OpenPGP keys unchanged. There is no public reset endpoint, email bypass or password-only fallback.

TOTP seeds use a separate `auth-keys` keyring beside SQLite, created only by explicit enrollment. Back up that complete directory with SQLite; credential-keys and browser OpenPGP exports remain separate. Missing auth keys fails closed for TOTP. A matching valid recovery code is an explicit alternative factor, never password-only fallback. Rollback from schema 13 needs a pre-upgrade backup and compatible binary; do not edit migration records. A database restore can resurrect revoked authentication state or consumed recovery codes: revoke restored sessions/challenges and replace the recovery set before relying on it. An older password reset must never be mistaken for removal of 2FA.
