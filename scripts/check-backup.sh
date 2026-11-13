#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Disposable Linux-container replication/restore drill. Never uses operator volumes.
set -eu
engine=${MAILSPACE_CONTAINER_CLI:-docker}
image=${MAILSPACE_BACKUP_TEST_IMAGE:-mailspace-backup-test:local}
qa_prefix="mailspace-backup-check-$(date +%s)-$$"
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cleanup() {
  "$engine" rm -f "$qa_prefix-app" "$qa_prefix-replication" >/dev/null 2>&1 || true
  for suffix in data replicas restored; do
    "$engine" volume rm "$qa_prefix-$suffix" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT HUP INT TERM
"$engine" build --target backup -t "$image" "$repo_dir"
for suffix in data replicas restored; do "$engine" volume create "$qa_prefix-$suffix" >/dev/null; done
printf '%s\n' 'Synthetic backup test password' | "$engine" run --rm -i -e MAILSPACE_DB=/app/data/mailspace.db -v "$qa_prefix-data:/app/data" --entrypoint /app/mailspace "$image" user add before@example.test
"$engine" run -d --name "$qa_prefix-app" -e MAILSPACE_DB=/app/data/mailspace.db -e MAILSPACE_ORIGIN=https://mail.example.test -v "$qa_prefix-data:/app/data" --entrypoint /app/mailspace "$image" >/dev/null
"$engine" run -d --name "$qa_prefix-replication" -v "$qa_prefix-data:/app/data" -v "$qa_prefix-replicas:/backups" -v "$repo_dir/deploy/litestream.yml:/etc/litestream.yml:ro" -e LITESTREAM_REPLICA_URL=file:///backups/mailspace "$image" >/dev/null
attempt=0
while ! "$engine" exec "$qa_prefix-replication" sh -c 'test -n "$(find /backups -type f -print -quit)"'; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then "$engine" logs "$qa_prefix-replication"; exit 1; fi
  sleep 1
done
# This write occurs after the initial replica exists, exercising ongoing WAL sync.
printf '%s\n' 'Synthetic later backup password' | "$engine" run --rm -i -e MAILSPACE_DB=/app/data/mailspace.db -v "$qa_prefix-data:/app/data" --entrypoint /app/mailspace "$image" user add after@example.test
sleep 3
"$engine" stop --time 10 "$qa_prefix-app" >/dev/null
"$engine" stop --time 10 "$qa_prefix-replication" >/dev/null
"$engine" run --rm -v "$qa_prefix-restored:/app/data" -v "$qa_prefix-replicas:/backups" -v "$repo_dir/deploy/litestream.yml:/etc/litestream.yml:ro" -e LITESTREAM_REPLICA_URL=file:///backups/mailspace "$image" restore -config /etc/litestream.yml -o /app/data/mailspace.db /app/data/mailspace.db
"$engine" run --rm -e MAILSPACE_DB=/app/data/mailspace.db -v "$qa_prefix-restored:/app/data" --entrypoint /app/mailspace "$image" user revoke-sessions after@example.test
printf '%s\n' 'PASS: ongoing SQLite writes recovered into a fresh volume.'
