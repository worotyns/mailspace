# 1.0 completion plan

This is an implementation checklist, not a release declaration. RELEASE_READINESS.md remains the source of truth for passed gates. Each slice needs behavior/security tests and a separate reviewable commit. No automatic stable tag or release until these gates are satisfied.

## Ordered slices

1. **MS-006 compatibility:** binary OpenPGP input, MIME transfer encodings, detached multipart/signed verification with original signed octets, safe quoting and compatibility fixtures. Preserve valid/untrusted/invalid/unknown labels; no private key or silent plaintext export. Run GnuPG and actual Thunderbird acceptance separately.
2. **MS-003/MS-018 mail operations:** charset/large-mail and reconnect acceptance, visible incomplete history/storage budgets, older-history pagination and controlled retention. Preserve account generation/UIDVALIDITY and SMTP unknown-result behavior.
3. **MS-009/MS-010 team use:** explicit source grants, organization membership plus source ACL on every read/download/job, revocation and separate send-as. No implicit private-mail access from a Space/model match; encrypted sharing never transfers keys.
4. **Release security and accessibility:** review authorization/session/SSRF/MIME/export boundaries, keyboard/focus/mobile tests and dependency/license/vulnerability checks. Record findings and fixes; do not call an internal review an independent audit.
5. **Operations and packaging:** upgrade/rollback, database plus installation keyring restore, browser export recovery, executed release/multiarch smoke and a clearly labelled prerelease before stable 1.0.

Optional Drive, full-text/semantic indexing, AI compose, PostgreSQL and YunoHost are follow-up modules, not mandatory dependencies or pretend-complete features. JEV/OpenRouter and Litestream remain opt-in, independently tested boundaries. Live paid-provider availability and real external-client acceptance must be recorded honestly, never inferred from mocks.

## Current evidence

Main f2266a2 has passing GitHub CI, including crypto tests, Go race tests, Docker build and continuous SQLite write/restore drill. 1.0 compatibility/team/security gates remain open. The implementation branch is release/1.0-readiness; each completed slice updates this record and RELEASE_READINESS.md before publication.

### MS-006 first compatibility slice

Binary OpenPGP/MIME input is implemented locally (ADR-0023), including MIME transfer-encoding preservation and authenticated local decrypt. Detached signatures/secure quoting/Thunderbird acceptance remain open; no full crypto or 1.0 completion claim.
