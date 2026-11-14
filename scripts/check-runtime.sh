#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Disposable runtime acceptance. Never mounts operator data or exposes a host port.
set -eu
engine=${MAILSPACE_CONTAINER_CLI:-docker}
image=${MAILSPACE_RUNTIME_TEST_IMAGE:-mailspace-runtime-test:local}
qa_prefix="mailspace-runtime-check-$(date +%s)-$$"
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cleanup() {
  "$engine" rm -f "$qa_prefix" >/dev/null 2>&1 || true
  "$engine" volume rm "$qa_prefix-data" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM
"$engine" build -t "$image" "$repo_dir"
"$engine" volume create "$qa_prefix-data" >/dev/null
printf '%s\n' 'Synthetic runtime test password' | "$engine" run --rm -i -e MAILSPACE_DB=/app/data/mailspace.db -v "$qa_prefix-data:/app/data" "$image" user add runtime@example.test >/dev/null
"$engine" run -d --name "$qa_prefix" --read-only --cap-drop ALL --security-opt no-new-privileges --tmpfs /tmp -e MAILSPACE_DB=/app/data/mailspace.db -e MAILSPACE_ORIGIN=https://mail.example.test -v "$qa_prefix-data:/app/data" "$image" >/dev/null
health() {
  attempt=0
  until "$engine" exec "$qa_prefix" wget -q -O /dev/null http://127.0.0.1:8080/api/health 2>/dev/null; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 20 ]; then "$engine" logs "$qa_prefix"; return 1; fi
    sleep 1
  done
}
health
uid=$("$engine" exec "$qa_prefix" id -u)
if [ "$uid" = 0 ]; then printf '%s\n' 'FAIL: runtime must not run as root.' >&2; exit 1; fi
if "$engine" exec "$qa_prefix" touch /app/runtime-write-probe >/dev/null 2>&1; then
  printf '%s\n' 'FAIL: runtime application directory is writable.' >&2
  exit 1
fi
for path in login assets/crypto/message-text.mjs assets/vendor/marked/marked.esm.js source.tar.gz; do
  "$engine" exec "$qa_prefix" wget -q -O /dev/null "http://127.0.0.1:8080/$path"
done
"$engine" exec "$qa_prefix" sh -c 'wget -q -O - http://127.0.0.1:8080/assets/THIRD_PARTY_NOTICES.txt | grep -q marked'
# Do not mistake an anonymous login redirect for an authorized API response.
if "$engine" exec "$qa_prefix" wget -q -O /dev/null http://127.0.0.1:8080/api/crypto/session 2>/dev/null; then
  printf '%s\n' 'FAIL: anonymous crypto-session API access succeeded.' >&2
  exit 1
fi
"$engine" restart "$qa_prefix" >/dev/null
health
"$engine" exec "$qa_prefix" /app/mailspace user revoke-sessions runtime@example.test >/dev/null
printf '%s\n' 'PASS: non-root read-only runtime, local assets/source/notices, anonymous denial and persistent user after restart.'
