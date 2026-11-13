# Environment configuration

Copy `.env.example` to ignored `.env` and replace the examples before `docker compose up`. Compose reads `.env`; the native binary does not. Native deployments must export these variables through their process manager. Never commit the populated `.env`, provider/API tokens or private key files.

## Core application

| Variable | Meaning | Default / example |
|---|---|---|
| `MAILSPACE_MODE` | Runtime operating mode. The only supported mode is `personal`: provisioned users, authentication, private mailboxes and persistent data. It does not choose a UI, organization or AI provider. Multiple provisioned logins have isolated private data; team mailbox sharing is not implemented. The old unauthenticated demo mode is removed; use `make dev-sandbox` for synthetic mail. | Native default `personal`; Compose sets `personal`. Leave it unchanged. |
| `MAILSPACE_DB` | SQLite database **file path**, inside the container when using Docker. Keep it on the persistent data volume. | Compose `/app/data/mailspace.db`; native `data/mailspace.db`. |
| `MAILSPACE_ORIGIN` | Exact public address used in the browser. Controls Secure cookies and Origin/CSRF validation. Required. No trailing slash/subpath. This is HTTPS even when the proxy forwards over internal HTTP. | `https://mail.example.com` |
| `MAILSPACE_ADDR` | Address the Go HTTP process listens on. Distinct from the public browser origin. The image sets this automatically. | Native `127.0.0.1:8080`; image `0.0.0.0:8080` inside its network. Compose publishes only host loopback. |
| `MAILSPACE_DATA_VOLUME` | Persistent Docker named volume containing SQLite and the credential keyring. Keep stable across upgrades. For an existing custom installation use its actual volume name. Not read by the native binary. | `mailspace_mailspace-data` |
| `MAILSPACE_WEB_DIR` | Directory containing the corresponding `source.tar.gz` offered by the app. UI assets are embedded in the binary. | `dist`; image `/app/dist` through its working directory. |
| `MAILSPACE_CREDENTIAL_KEYS` | Directory containing installation keys that encrypt saved IMAP/SMTP credentials. Preserve with the database. This is **not** the browser OpenPGP vault. Native override; default Compose uses its normal data directory. | `credential-keys` beside the DB; image `/app/data/credential-keys`. |
| `MAILSPACE_INSECURE_HTTP` | Explicit local development exception to HTTPS/Secure-cookie requirements; only a loopback HTTP origin is accepted. Keep unset in production. Not passed by the production Compose file. | Unset/`0`; native local development only: `1`. |
| `MAILSPACE_DEV` | Native developer option for reading templates/assets from disk. Not a production setting and not an alternative to authentication. | Unset; use `make dev` for the development fixture. |

`MAILSPACE_MODE` and `MAILSPACE_DB` are literal values in the supplied Compose file, not `${...}` substitutions. Putting overrides for them in `.env` has no effect on that file; edit the Compose environment or use a Compose override if necessary. Keep the database under `/app/data` for persistence.

The `${NAME:-}` strings in Compose mean “use the environment value, or an empty string if unset.” `${NAME:?message}` means configuration must fail with that message when it is empty. These are Compose substitutions, not values you should paste literally into `.env`.

## Provider connection policy

These operator settings approve destinations; they are **not account credentials**. Each user enters their email address and IMAP/SMTP login in account setup.

| Variable | Meaning | Example |
|---|---|---|
| `MAILSPACE_MAIL_ENDPOINTS` | Comma-separated exact `hostname:port` pairs approved for **public** IMAP/SMTP servers. Public-policy DNS resolution still rejects private/special IPs. Empty means no public provider connections. Both incoming and outgoing destinations must be approved. No wildcards. | `imap.example.com:993,smtp.example.com:465` or SMTP `:587` with required STARTTLS selected in the UI. |
| `MAILSPACE_PRIVATE_MAIL_ENDPOINTS` | Separate explicit exception for mail servers on a private/LAN/container/loopback network. Use exact reachable hostnames and ports; this is not a broad network range. Empty means no private provider connections. | `mail.internal.example:993,mail.internal.example:465` |
| `MAILSPACE_MAIL_CA_FILE` | Optional **public CA certificate** PEM path to trust a private mail server, in addition to normal system CAs. The file must also be mounted read-only into the container. Hostname and TLS verification remain enabled. Never point this at a private key. This setting applies to mail transport, not classifier HTTPS. | Empty for normal public providers; `/app/certs/provider-ca.pem` with a matching mount for a private CA. |

`localhost`/`127.0.0.1` inside a container refers to that container, not the host or another service. Provider credentials are stored encrypted after Test & save; no provider password belongs in these allowlist values. See MAIL_ACCOUNTS.md.

## Optional JEV / Space classification

All JEV variables are optional. Empty `MAILSPACE_JEV_PROVIDER` means no classifier client, worker or provider requests. Configuring a provider does not opt users in: each user must explicitly choose Manual, Review or Auto and the permitted payload in **Space automation**.

| Variable | Meaning | Example / default |
|---|---|---|
| `MAILSPACE_JEV_PROVIDER` | Classifier adapter. `openrouter` uses the fixed HTTPS OpenRouter chat-completions endpoint; `local` uses the explicit compatible endpoint below. | Empty = disabled; `openrouter` or `local`. |
| `MAILSPACE_JEV_MODEL` | Exact model identifier understood by that endpoint. Must support strict structured JSON responses. OpenRouter routing additionally requires support for the configured privacy policy. No default model or automatic fallback to a weaker policy. | Your chosen provider/model identifier; required when provider enabled. |
| `MAILSPACE_JEV_API_TOKEN` | Direct API token supplied by the operator. Passed only to the classifier request; never shown in settings or stored in decision traces. Environment values may be visible to container administrators through inspection. Keep the real value only in ignored `.env` or a secret manager. | Empty; set this **or** KEY_FILE. |
| `MAILSPACE_JEV_KEY_FILE` | Alternative private token file readable by the app user, with mode `0600`. Path is inside the container. Avoids exposing the token value in container environment inspection. Setting both token and file is rejected. | `/app/data/openrouter-api-key`; empty when using API_TOKEN. |
| `MAILSPACE_JEV_ENDPOINT` | Used only for `local`: full OpenAI-compatible **chat-completions** endpoint URL. No redirects/proxy inheritance. Plain HTTP is allowed only for numeric loopback addresses; all other endpoints require verified HTTPS. It is an operator-controlled endpoint, never a user-supplied fetch URL. Ignored for OpenRouter. | Native/same-container local example: `http://127.0.0.1:11434/v1/chat/completions`. |

Minimal OpenRouter configuration:

```dotenv
MAILSPACE_JEV_PROVIDER=openrouter
MAILSPACE_JEV_MODEL=your-selected-provider/model-id
MAILSPACE_JEV_API_TOKEN=your-token-in-this-private-env-file
MAILSPACE_JEV_KEY_FILE=
MAILSPACE_JEV_ENDPOINT=
```

The model/token above are placeholders. Choose a model supporting JSON schema and the required OpenRouter routing policy. Set a provider spending limit before enabling automatic mode. OpenRouter requests use `data_collection: deny`, `zdr: true` and `require_parameters: true`; unsupported routes fail closed.

For the file alternative, leave API_TOKEN empty. To populate an app-owned file from an existing private file on the server (without putting the token in command arguments):

```sh
docker compose run --rm --no-deps -T --entrypoint sh mailspace -c 'umask 077; cat > /app/data/openrouter-api-key' < /path/to/your/private-token-file
```

Then set KEY_FILE to that path and restart the app. Never paste a live token into chat/issues/screenshots. Automatic classification excludes encrypted messages. Explicit classification of a locally decrypted selection requires separate per-request consent and human approval; configuring these variables does not enable that export.

## Optional continuous SQLite backup

The normal `compose.yaml` never starts Litestream. It is enabled only with the explicit `compose.backup.yaml` overlay **and** a configured replica URL. With no backup environment/configuration there is no backup process and no remote backup traffic.

| Variable | Meaning | Example / default |
|---|---|---|
| `LITESTREAM_REPLICA_URL` | Private destination for the continuously replicated SQLite WAL/snapshots. Required by the backup overlay. A local replica survives container replacement but not loss of the host. | `file:///backups/mailspace` for local testing; `s3://your-private-bucket/mailspace/database` for off-host storage. |
| `MAILSPACE_BACKUP_VOLUME` | Docker named volume used by the example file replica at `/backups`. Relevant to file replicas; do not casually change it. | `mailspace_mailspace-replicas` |
| `AWS_ACCESS_KEY_ID` | S3 credential for the backup process only. Not passed to the web application. Leave blank for file replicas or use your platform's supported IAM credential flow. | Empty. |
| `AWS_SECRET_ACCESS_KEY` | Matching S3 secret; keep out of Git and screenshots. | Empty. |
| `AWS_REGION` | Region for the S3 replica. | `us-east-1` example; use your bucket's actual region. |

Start explicitly:

```sh
docker compose -f compose.yaml -f compose.backup.yaml up -d --build
```

Litestream backs up SQLite, **not installation credential-key files, browser OpenPGP keys or local drafts**. Preserve the server keyring separately on installation/rotation, and export browser keys separately. Keep a separate keyring backup and verify recovery into a fresh installation before relying on replication. Configuring a replica is not proof that backups are current; monitor and periodically restore-test them.
