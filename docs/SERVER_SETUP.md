# Deploy Mailspace on your server

This guide is for a single-instance Linux server with Docker Engine and Docker Compose v2. Mailspace uses an existing IMAP/SMTP provider; it is not an email server. The UI is at `/`, not a subpath. No model, indexing service or external AI account is needed.

Current target: an authenticated personal installation for testing, with real mail transport, persistent SQLite, private Spaces/labels, local OpenPGP and ordinary replies. Do not mistake this for a completed 1.0/security audit. See RELEASE_READINESS.md for the remaining gates. Organizations do not share mail yet.

All settings and defaults are documented in [CONFIGURATION.md](CONFIGURATION.md). Optional classification: [AUTOMATION.md](AUTOMATION.md). Continuous SQLite replication: [BACKUPS.md](BACKUPS.md).

## 1. Prepare the server

Use a dedicated domain such as `mail.example.com`. Point its DNS to your server. You need Docker/Compose, outbound access to the approved provider ports, and an HTTPS reverse proxy. The application port stays bound to `127.0.0.1:8080`; never publish it directly to the Internet. Open ports 80/443 for the proxy as appropriate. Allow sufficient disk for cached mail, outbox and backups; monitor volume growth. Run one app instance per SQLite volume.

Copy the repository or extract the provided `mailspace-source.tar.gz` and enter the directory containing compose.yaml. For the prefixed source bundle:

```sh
tar -xzf mailspace-source.tar.gz
cd mailspace
cp .env.example .env
```

Edit `.env` before starting. For an existing installation with a custom Compose project name, set MAILSPACE_DATA_VOLUME to the actual existing volume name (inspect `docker volume ls`); the new default cannot discover a differently named installation automatically:

```dotenv
MAILSPACE_ORIGIN=https://mail.example.com
MAILSPACE_MAIL_ENDPOINTS=imap.your-provider.com:993,smtp.your-provider.com:465
MAILSPACE_PRIVATE_MAIL_ENDPOINTS=
MAILSPACE_MAIL_CA_FILE=
MAILSPACE_DATA_VOLUME=mailspace_mailspace-data
```

Replace the domain and exact provider host:port pairs with actual values. No trailing slash/path in the origin. This is the browser-facing HTTPS address, even though the reverse proxy talks HTTP to the container. Provider passwords and private OpenPGP keys never belong in this file. Empty endpoint lists intentionally disable provider connections. SMTP submission on port 587 is supported with required STARTTLS; configure the exact port here and select STARTTLS in account setup. Providers requiring OAuth-only login are not yet supported; use an app password where your provider supports it.

For a private/LAN mail server put its exact hostname:port in MAILSPACE_PRIVATE_MAIL_ENDPOINTS instead. This is an explicit administrator exception, not a wildcard. `localhost` inside a container refers to that container. A mail server in another container needs a reachable service hostname and a certificate valid for that hostname. TLS verification is always enabled.

## 2. Build and provision your login

```sh
docker compose config --quiet
docker compose build
docker compose run --rm --no-deps mailspace user add you@example.com
docker compose up -d
```

Enter a new Mailspace login password at the hidden terminal prompt (at least 12 characters). No default admin/demo password exists. The app login is separate from your IMAP/SMTP password. Node and Go are build stages only; the runtime container needs neither. The named volume is initialized for the non-root app user and survives restart, rebuild and `docker compose down`.

```sh
docker compose ps
curl --fail http://127.0.0.1:8080/api/health
curl --head http://127.0.0.1:8080/
```

The health endpoint returns personal mode. An unauthenticated `/` redirects to `/login`; it must not expose synthetic mail. The container healthcheck is process liveness, not proof of provider delivery. If startup fails, inspect `docker compose logs --tail=100 mailspace`. Do not enable request-body logging at your proxy.

## 3. Add HTTPS

Use your existing reverse proxy with a valid certificate, forwarding to `127.0.0.1:8080`. Preserve the Host and Origin headers, disable caching of app responses, allow request bodies up to 8 MiB, and allow at least 60 seconds for provider checks. Do not rewrite paths or strip security headers. No forwarded-header trust or insecure-cookie override is needed: MAILSPACE_ORIGIN defines the public origin.

For an already installed Caddy on the same host, the site configuration is:

```caddyfile
mail.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Use your real domain and reload the proxy through its normal configuration-validation process. If the proxy runs in a container, its `127.0.0.1` is a different network namespace: connect it to the app network and target `mailspace:8080`, keeping the app unexposed publicly. Do not solve this by binding the app port to every host interface.

Open `https://mail.example.com/` and sign in. Plain HTTP with the production HTTPS origin cannot retain the Secure session cookie; use HTTPS in your browser. The HTTP exception is deliberately limited to explicit local development and is absent from production Compose.

## 4. Connect your actual mailbox

Settings → Connect mail account. Enter the email address, incoming IMAP hostname/port, TLS mode, provider username/password, SMTP hostname/port/TLS mode, and the exact existing Sent folder name. Choose same credentials only if appropriate. Typical implicit TLS ports are IMAP 993 / SMTP 465; SMTP 587 must use STARTTLS. Do not select a host that is missing from the operator allowlist.

Test & save authenticates both connections over verified TLS, saves credentials encrypted under a separate installation keyring and schedules synchronization. No test email is sent by the connection check. The timeline shows cached recent messages as the bounded sync completes. Refresh requests another sync; it is not a promise of complete mailbox history. If needed use Settings → Check connection, verify endpoint allowlists and Sent folder name, and inspect the safe error shown by the form.

Create a Space with a name and relevance description. Unsorted → a message → Spaces & labels assigns that individual message to one or multiple Spaces. The dialog groups adjacent messages visually, without merging memberships. View email thread shows related messages across the current account's cached folders/outbox; replies from that view target the selected original and keep its References/In-Reply-To. Similar subjects alone never merge threads. Missing/ambiguous References and older uncached mail limit the preview.

Try one ordinary reply to your own address, verify receipt using another client and check the accepted/unknown result and Sent copy. Server acceptance is not recipient delivery. An unknown send is never automatically retried: check the provider before composing another copy.

## 5. Configure optional local encryption

Local keys → import a protected private key or generate one → export a protected backup. Import the recipient public key and compare the complete fingerprint independently before marking the email/key binding trusted. Your own From address also needs a verified binding. Keys and passphrases remain browser-local. Different browsers, devices, origins and cleared site data have separate vaults. A domain change requires exporting/importing local keys and drafts.

Write inline first, then select Encrypt. Unlock in the small modal; sending is a separate action. Missing/changed/untrusted keys block encryption. Encrypted originals cannot silently become plaintext replies. Advanced encrypted composer supports encrypted local drafts and attachments; these are not server-side drafts. Preserve your exports separately: the server backup cannot recover the browser vault, passphrase or local drafts. Current OpenPGP interoperability limits are explicit in RELEASE_READINESS.md.

## 6. Persistence and backup

The named volume contains the SQLite database, its possible WAL/SHM files and the credential keyring beside it. Keep them together. Ordinary mail cache/outbox and metadata are sensitive; server credential encryption is not whole-database encryption. Browser OpenPGP vault/drafts are outside this volume.

For an offline backup, stop all app/provisioning processes using this volume. Use a new output filename; never overwrite your last known-good backup:

```sh
umask 077
mkdir -p backups
docker compose stop mailspace
(
  set -C
  docker compose run --rm --no-deps -T --entrypoint sh mailspace -c 'tar -C /app/data -czf - .' > backups/mailspace-before-upgrade.backup.tar.gz
)
tar -tzf backups/mailspace-before-upgrade.backup.tar.gz
docker compose start mailspace
```

If tar fails, discard the incomplete new file and restart the original installation; do not treat an empty/failed archive as a backup. Retain the matching source bundle/commit, built image and `.env`/proxy configuration securely. Keep encrypted copies off the server and test restoration before relying on backups. Never copy only a live SQLite file. `docker compose down -v` deletes the volume and is not a normal stop/upgrade command.

## 7. Test restore and rollback

Restore into a **new empty named volume**, retaining the original. Stop the current service. In `.env`, change MAILSPACE_DATA_VOLUME to a new name such as `mailspace-restore-check`. Use the image/source version matching the backup, especially when rolling back a schema migration.

```sh
docker compose stop mailspace
docker compose run --rm --no-deps -T --entrypoint sh mailspace -c 'tar -xzf - -C /app/data' < backups/mailspace-before-upgrade.backup.tar.gz
docker compose run --rm --no-deps mailspace user revoke-sessions you@example.com
docker compose up -d
```

Repeat session revocation for every provisioned user: an old backup may contain sessions revoked since it was taken. Verify login, Spaces/labels, account ownership, provider checks and a controlled send before accepting the restored installation. Browser vault recovery is a separate import. Keep the new volume name in `.env`; otherwise a later command could select the old volume. Do not extract over an existing installation or mix database and keyring generations. The app refuses a database schema newer than its binary. There are no destructive down-migrations.

## 8. Upgrade

1. Read release notes and RELEASE_READINESS.md; keep the old source/image and a verified offline backup.
2. Stop the app; deploy the new source and build with `docker compose build`. Keep `.env`, volume name and proxy configuration.
3. Start `docker compose up -d`. Migrations run transactionally at startup. Check health, login, Spaces and mailbox settings; perform a controlled send.
4. If rollback is needed, restore the complete pre-upgrade snapshot into a new empty volume with its matching old image. Merely starting an older binary against the upgraded DB is not a rollback.

No published registry image or GitHub release is assumed by this guide. Local build/source distribution works without a Git remote. Optional AI, decision models, team sharing, indexing, PostgreSQL and YunoHost are not secretly installed.

## 9. Login recovery and session revocation

Operator-only local commands use the same configured volume/DB. They are not public password-reset endpoints:

```sh
docker compose run --rm --no-deps mailspace user password you@example.com
docker compose run --rm --no-deps mailspace user revoke-sessions you@example.com
```

The password command prompts privately and commits the password change plus all that user's session revocations atomically. It does not change provider passwords, OpenPGP keys or their passphrases. Run `user add` again with another email to provision another isolated user. There is no public signup, MFA, SMTP invitation mail or shared-mail ACL implementation yet.

## Troubleshooting

- Login loops/403: exact HTTPS origin, valid certificate, original Host/Origin preserved, no proxy caching. Do not disable CSRF/cookies to work around proxy errors.
- Provider denied: exact hostname/port allowlist, public versus explicit private policy, DNS reachability, CA trust and TLS mode, app-password policy. Reconfigure through Settings; never disable TLS verification.
- Custom CA: mount the public PEM read-only into `/app/certs/provider-ca.pem` with an additional Compose override and set MAILSPACE_MAIL_CA_FILE to that in-container path. Never mount a private CA/server key.
- Empty/partial history: synchronization is bounded recent mail; older history/native IDLE remains open. Missing References/duplicate Message-ID values intentionally do not become guessed threads.
- Cannot unlock a key: local vault/browser/origin and OpenPGP passphrase, not the app password. Restore a protected exported key if needed; the administrator cannot decrypt/reset it.
- Database/key errors after restore: stop and restore a complete matching snapshot. Do not delete keyring files or create a fresh keyring next to an old DB.
