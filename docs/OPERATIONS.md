# Operations and releases

The canonical operator runbook is [SERVER_SETUP.md](SERVER_SETUP.md). It covers the authenticated stateful application; the old M0 unauthenticated demo/no-persistence instructions are retired. [RELEASE_READINESS.md](RELEASE_READINESS.md) lists current limits and open 1.0 gates.

## Packaging

Docker Compose now defaults to authenticated personal mode, with explicit HTTPS origin and a persistent named volume. `compose.personal.yaml` remains only as a compatibility overlay for existing installs; it is unnecessary for a new install. Keep the same volume name when upgrading. `make dev` / `make dev-sandbox` use isolated synthetic providers and the actual product UI, never a second legacy presentation.

Node builds local assets; Go embeds the UI; runtime needs neither Node nor a separate web directory. A build includes corresponding source and upstream license notices. Runtime is non-root, read-only except the private named data volume and temporary storage. One app instance per SQLite installation.

## Release workflows

CI builds locked assets, runs formatting/client tests/vet/race, validates Compose and builds the image. Release validation uses a synthetic HTTPS origin; an anonymous root redirects to login. The release workflow builds Linux/macOS amd64/arm64 binaries, matching source/checksums and a multiarch GHCR image with SBOM/provenance. macOS is unsigned/not notarized; cross-compilation is not a native runtime compatibility claim.

Publish only after green CI, secret/license/dependency review and reviewed release notes. The first release remains a prerelease for controlled testing until the readiness gates are met. The testing source is published at https://github.com/worotyns/mailspace. No release tag, GitHub Release or GHCR publication is implied by an ordinary source push. The `release` environment may enforce required reviewers; no PAT/provider secrets belong in CI.

## Source and recovery

Own code AGPL-3.0-only. `/source.tar.gz` must correspond to the deployed build and include dependency notices. `make build` archives HEAD, so commit first. Docker build context/source bundles exclude `.env`, data and backups. Protect installation backups: they contain ordinary cached mail, outbox/envelope metadata and the credential keyring. Browser OpenPGP exports/local drafts are separate.

Migrations are forward-only and transactional. Older binaries refuse newer schemas; rollback requires the matching complete pre-upgrade DB/keyring snapshot and old image/source. Test restore before relying on a backup. Never restore over a live database or retain stale WAL files from a different generation.

YunoHost packaging, PostgreSQL and online backup tooling remain future work; no catalog availability or SSO integration is implied.
