# IMAP/SMTP account setup — MS-002

Personal mode can verify and store provider account settings. Read-only synchronization is available (MAIL_SYNC.md); a connection check alone does not claim message delivery. Ordinary sending/replies and outbox certainty states are documented in SENDING.md.

## Operator configuration

```sh
MAILSPACE_MAIL_ENDPOINTS=imap.example.test:993,smtp.example.test:465
```

Replace these example names with your provider's actual endpoints. Empty allowlists mean no outbound mail connections. Ports are exact. Public approvals still reject private/special DNS results. To use a private mail server, explicitly configure MAILSPACE_PRIVATE_MAIL_ENDPOINTS instead. Do not approve unrelated services or broad network ranges.

MAILSPACE_MAIL_CA_FILE optionally points to a public CA certificate PEM file (under 1 MiB), added to system trust. It is not a private key or a setting to disable verification. Hostname/certificate checks remain mandatory. SMTP currently uses password-based AUTH PLAIN over TLS; IMAP AUTH PLAIN/LOGIN. OAuth-only providers are not supported yet.

In Docker, the personal overlay passes endpoint lists. A custom CA must also be mounted read-only and its in-container path configured; it is not automatically copied from the host. Docker execution remains pending CI validation in this environment.

## User flow

Sign in → Connect mail account → enter address, incoming/outgoing server settings and provider login → Test & save. Both protocols authenticate over verified TLS before a transaction saves anything. The SMTP password may reuse IMAP credentials only when the user explicitly selects that option. No message is sent by a connection check.

Settings show usernames and endpoint metadata but never saved passwords. Updating requires re-entering provider passwords and re-verifying both connections. Failed probes save no partial account. Existing check/setting routes enforce owner access; another user receives 404 without reaching a connector.

## Encrypted credential vault and rotation

Default key directory: data/credential-keys (relative to the DB directory). Override with MAILSPACE_CREDENTIAL_KEYS. Directory 0700, key files 0600. The vault protects provider login data under an installation key outside SQLite; it is not the OpenPGP key vault.

For rotation, stop the application, keep an offline backup of the complete DB and key directory, then run:

```sh
./mailspace credentials rotate
```

Use the same MAILSPACE_DB and MAILSPACE_CREDENTIAL_KEYS values as the running app. Start the app again and check connections. Old keys are intentionally retained. If a rotation fails, retain every key file and retry; rows include their key ids, so rollback does not discard access. Never restore a DB without its corresponding key directory. Missing keys cause a clear failure rather than silent key regeneration. Restoring an old backup can resurrect revoked app sessions; revoke them after restore.

## Local seeded mail, without a real mailbox

In one terminal:

```sh
make dev-mail
```

This creates ignored data/devmail state: a public CA, a TLS server key, a random password file and a monotonic UIDVALIDITY counter. Message contents are in memory and reset on restart. The fixture binds to loopback only and never relays mail. Six conversations/sixteen messages are real MIME; sample binary attachments become explicit text placeholders. The fake encrypted UI item is omitted until a real OpenPGP fixture exists.

Provision a personal app user (`./mailspace user add EMAIL`) and run the app in a second terminal:

```sh
MAILSPACE_MODE=personal MAILSPACE_ORIGIN=http://127.0.0.1:8080 MAILSPACE_INSECURE_HTTP=1 MAILSPACE_PRIVATE_MAIL_ENDPOINTS=127.0.0.1:1993,127.0.0.1:1465 MAILSPACE_MAIL_CA_FILE="$PWD/data/devmail/ca.pem" ./mailspace
```

In the setup form:

| Field | Value |
|---|---|
| Email and mail username | demo@example.test |
| IMAP | 127.0.0.1, port 1993, TLS |
| SMTP | 127.0.0.1, port 1465, TLS |
| Provider password | Read locally from data/devmail/password |
| Same SMTP credentials | Selected |

The app login password and fixture provider password are separate. The generated fixture password is not printed in server logs. This native local workflow is distinct from Docker deployment; it does not disable TLS verification.
