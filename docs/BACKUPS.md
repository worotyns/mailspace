# Continuous SQLite backup with Litestream

**Disabled by default.** Base `compose.yaml` has no backup process. Opt in by configuring a private destination and supplying `compose.backup.yaml`. Mailspace does not discover buckets, create public storage or send backup traffic without this configuration. The optional sidecar pins Litestream 0.5.18; default app runtime does not include it.

## Enable explicitly

In ignored `.env`, choose one destination:

```dotenv
# Local test replica: survives container replacement, not loss of this host.
LITESTREAM_REPLICA_URL=file:///backups/mailspace
MAILSPACE_BACKUP_VOLUME=mailspace_mailspace-replicas
AWS_ACCESS_KEY_ID=
AWS_SECRET_ACCESS_KEY=
AWS_REGION=us-east-1
```

For off-host storage use your private S3 bucket prefix (`s3://your-private-bucket/mailspace/database`), actual bucket region and narrowly scoped credentials. Bucket policy/encryption/credentials are your responsibility. Use a separate prefix per installation. This configuration does not provide client-side replica encryption. Never place secrets in a public repository. Only the sidecar receives the backup credential variables.

```sh
docker compose -f compose.yaml -f compose.backup.yaml config --quiet
docker compose -f compose.yaml -f compose.backup.yaml up -d --build
docker compose -f compose.yaml -f compose.backup.yaml logs --tail=100 litestream
```

The sidecar shares `/app/data` as the app user and continuously reads SQLite WAL. Example config: sync every second, snapshot interval 24h, snapshot retention 7 days, local L0 retention 24h. These are targets, not an SLA: outages, disk pressure and unavailable storage can increase loss. No metrics listener or MCP server is enabled. See [upstream configuration](https://litestream.io/reference/config/) for destination options.

## What must also be preserved

Litestream backs up **SQLite only**. Preserve `/app/data/credential-keys/` separately at installation and every credential-key rotation, with restricted permissions and an encrypted off-host copy. Without the matching installation keyring the restored provider credentials cannot be decrypted. Keep the latest complete offline backup from [SERVER_SETUP.md](SERVER_SETUP.md), source/image version and private environment/proxy configuration. Browser OpenPGP vaults, drafts and passphrases need separate protected exports from each browser. SQLite includes sensitive ordinary mail and relationship metadata; secure replicas accordingly.

## Restore drill into a fresh volume

Do not overwrite a live DB or test against your only installation. Retain original volume and known-good replicas. Stop app writers first, let Litestream finish, check its logs, then stop it. Use the same overlay for these commands:

```sh
docker compose -f compose.yaml -f compose.backup.yaml stop mailspace
docker compose -f compose.yaml -f compose.backup.yaml stop litestream
```

Change `MAILSPACE_DATA_VOLUME` in the private `.env` to a **new empty** volume name, e.g. `mailspace-restore-check`. Keep the replica URL/backup volume unchanged and the matching source/image version. Initialize the empty volume with the app image's ownership:

```sh
docker compose run --rm --no-deps --entrypoint sh mailspace -c 'test ! -e /app/data/mailspace.db'
docker compose -f compose.yaml -f compose.backup.yaml run --rm --no-deps litestream restore -config /etc/litestream.yml -o /app/data/mailspace.db /app/data/mailspace.db
```

Restore the separately preserved **matching credential-key directory**, with app-readable ownership and private file modes, into `/app/data/credential-keys`. Do not generate a replacement keyring and expect it to decrypt old credentials. Follow [upstream restore reference](https://litestream.io/reference/restore/) for point-in-time recovery; the command above restores the latest available database, which can predate the final write.

Revoke restored sessions for every provisioned user using `docker compose run --rm --no-deps mailspace user revoke-sessions you@example.com`. Start the app alone, verify login, data/Spaces, provider connection and a controlled send; inspect health and logs. Confirm the new volume name stays in `.env`. Keep the old volume untouched. Only then resume replication for the accepted installation. Do not run two independent app databases into the same replica prefix.

Monitor sidecar restarts/errors, available disk, replica freshness and snapshot creation. Periodically repeat a fresh-volume restore and record the newest recovered write. Container health alone does not prove a current backup. `docker compose down -v` is destructive. Starting an old binary against a new schema is not rollback: restore the matching pre-upgrade database/keyring and source version.

## Repeat the synthetic drill

Run `scripts/check-backup.sh` with Docker, or `MAILSPACE_CONTAINER_CLI=podman scripts/check-backup.sh` with Podman. It builds the optional image, creates isolated volumes, replicates an initial database plus a later write, restores into a fresh volume and verifies that later user exists. It removes only its own uniquely named resources. CI runs the same drill. This does not exercise your S3 account or replace an operator restore test with real installation keys.
