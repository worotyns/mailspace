# Walidacja — Go + HTMX + Tailwind · 2026-11-12

## Checki lokalne

- gofmt, go vet, go test -race — pass; testy API, nawigacji/HTML fragmentów, filtrów, malformed parameters, XSS escaping i polityki eksportu.
- npm locked install, Prettier dla Go templates/JS/CSS i build Tailwind/HTMX — pass.
- Zależności React/Mantine usunięte. npm audit przy instalacji: 0 znanych podatności w aktualnym drzewie (nie jest to audyt całego produktu).
- make help, seed, check i build — pass. Seed: 7 rozmów, 17 wiadomości, 4 kanały, 4 metadane załączników, 1 encrypted placeholder bez plaintextu.
- actionlint 1.7.12 dla CI/release — pass.
- Browser 1440×1000 i 390×844: stream/thread/classic, filtrowanie kanału, Enter/search, Escape/close, focus i mobile overflow — sprawdzone. Brak błędów/warnings w konsoli. Nie jest to pełny audyt accessibility.
- HTML full-page fallback i HX-History-Restore mają osobne testy. HTMX history cache wyłączony, Go autoescape zachowany.
- Binarka z osadzonymi assetami działa bez runtime Node i bez katalogu frontend; źródła dystrybuowane osobno jako dist/source.tar.gz.

## Granice

Brak Docker w lokalnym środowisku: compose config/build/runtime smoke muszą zostać wykonane przez CI. Workflows zostały sprawdzone statycznie, ale nie uruchomione na GitHub. Nie opublikowano repo/taga/obrazu, SBOM ani provenance.

Połączenia IMAP/SMTP są weryfikowane w MS-002; synchronizacja, wysyłka i OpenPGP crypto pozostają w backlogu. Auth/SQLite działa w osobnym personal mode (MS-001 poniżej). Wyszukiwanie obejmuje wyłącznie syntetyczne fixtures. Nie ma testów interoperacyjności, bo transport i crypto nie są jeszcze zaimplementowane. UI nie udaje wysyłki i pobierania załączników. Boundary przyszłego browser crypto: CLIENT_CRYPTO.md.

## MS-001 — personal identity foundation

Go race tests: Argon2id policy, SQLite permissions/WAL/FK, persistence after restart, duplicate identity denial, offline restore, newer-schema rejection, two-user account isolation, token digests, expiry/revocation, cookie flags, login rotation, Origin/CSRF denial and rate-limit bounds. Browser: synthetic CLI-provisioned user login, logout-all and protected page redirect; 390px overflow check. Temporary QA DB removed afterward. No real mailbox or actual user credentials used.

Go dependency notices are generated from the modules actually imported by the shipped binary. Docker overlay and build definitions have static validation only; Docker is still unavailable locally. Personal mode is an auth/storage foundation, not a functioning mail client yet.

## MS-002 — verified account setup

Unit/integration tests cover explicit endpoint approval, private/special/mixed DNS rejection, validated-IP pinning without second resolution, default-deny without a DNS query, TLS certificate rejection, required STARTTLS/no credentials before encryption, sanitized remote authentication errors, encrypted DB contents, AAD binding, durable key rotation and missing-key refusal, owner-only updates before probes, no password echo on errors/settings, and v1→v2 schema upgrade.

A real browser flow connected the production app to the isolated devmail fixture using its explicitly trusted CA, saved credentials, verified blank password fields, restarted the app after CLI credential-key rotation, and checked both connections again successfully. Settings were checked at mobile width without horizontal overflow. Disposable full-stack QA databases and TLS/password fixture files were removed afterward. No real mailbox or outside provider was contacted. Docker execution remains unverified locally.

## Toolchain security recheck

The first govulncheck run found reachable standard-library advisories in the host Go 1.27.1. The module minimum and Docker builder were raised to Go 1.27.4. A second full govulncheck run with the isolated Go 1.27.4 toolchain returned `No vulnerabilities found`. This is a known-vulnerability scan, not a security audit. The global host Go installation was unchanged.

## MS-003 — read-only mail

Real isolated TLS fixture: 13 INBOX messages, 3 folders and native MIME reached persistent storage and conversation/classic UI. Before/after IMAP flags identical. Tests cover reconnect without duplicate/redownload, expunge, flags, UIDVALIDITY reset, same UID across accounts, owner denial before network, durable partial download progress, stale config-generation denial and rollback of failed downloads. Paged fetch (>50 messages), oversized literal rejection, cancellation, MIME encryption/HTML/depth/text bounds, duplicate/missing/cyclic references and template escaping have regression tests. Browser: connect→refresh→open thread→Escape→classic; 1440×1000 and 390×844, no horizontal overflow or console warnings/errors. Screenshots saved as mailspace-live-imap-desktop/mobile.jpg. Only synthetic .test mail used. Docker remains unavailable.

Final MS-003 gate: make check (assets/formatting/vet/race/actionlint) passed on Go 1.27.4; govulncheck returned No vulnerabilities found. The oversized-literal regression caught a decoder shutdown deadlock; the transport now closes the socket, drains the streaming command, then waits for the decoder. Its regression test passes with a 20-second test timeout (normal runtime under a second). Disposable browser QA state removed after checking.

## MS-004 — SMTP/outbox

Scripted TLS and STARTTLS SMTP tests cover final acceptance, explicit rejection, lost final DATA response, timeout and rejected RCPT before DATA. Outbox race tests verify concurrent dispatch sends once, nonce replay/content conflict, foreign-user denial, stale configuration rejection, crash recovery without resend and independent unknown Sent-copy status. MIME tests cover envelope-only Bcc, Unicode/CRLF, reply references, limits, header injection, explicit plaintext choice and encrypted downgrade refusal.

Full integration: send from UI routes into the isolated .test SMTP sink, one IMAP APPEND into Sent, repeated POST still one record/one Sent copy, refresh reads it back. Browser: compose/send/outbox/refresh/Sent/read/reply at 390×844 and 1440×1000; no overflow or console warnings/errors. The browser check caught reply-to-self for own Sent messages; recipient selection now uses the original MIME To and has a regression test. Screenshots: mailspace-send-mobile.jpg and mailspace-sent-desktop.jpg. Disposable QA state removed. No real mail sent. Docker remains unavailable locally.

Final MS-004 gate: make check (formatting/assets/vet/race/actionlint) passed; govulncheck returned No vulnerabilities found on Go 1.27.4. Docker runtime remains unverified locally.

## MS-019 and organization foundation

Go regression tests cover calendar month/day validation, Monday-aligned grids, UTC→America/New_York day boundaries, padded-cell/month navigation, scoped account URLs, profile ownership and timezone validation, roster access/owner-only mutation, active-workspace switching, member revocation, private-account isolation, audit persistence and v1→v5 migration. The sandbox login has random private files and refuses missing credentials for existing state.

Browser: 1440×640 low-height shell has document height exactly 640, sidebar content 873 vs viewport 590 with independent overflow. 1440×900 and 390×844 Calendar/light/dark/settings show no horizontal overflow. Dark survives reload, all selectors synchronize, original theme preferences restored. Native profile save, organization create/add member/switch Personal/switch team work with synthetic users; no console warnings/errors. Final 8080 preview now runs cmd/devsandbox and is authenticated as a generated synthetic user; it is intentionally retained for user review. Disposable separate QA state was removed. Screenshots: mailspace-calendar-dark.jpg, mailspace-calendar-mobile-dark.jpg, mailspace-organization-mobile-dark.jpg and mailspace-organizations-dark.jpg. No real provider or personal data used. Docker remains unavailable.

Final UI/workspace gate: make check passed on Go 1.27.4 (asset build, formatting, vet, all race tests and actionlint); govulncheck returned No vulnerabilities found. Docker execution remains unavailable. The explicit synthetic preview marker and native authentication redirects are included in the checked code.

## MS-005 — local OpenPGP key vault (2026-11-13)

Full make check passed: locally bundled asset build, formatting, eight browser-module tests (including real isolated GnuPG 2.2.41 interop), Go vet/race and actionlint. Protected key persistence/reopen/export, malformed or mixed-protection imports, lock races, namespace separation, expiry/revocation and explicit fingerprint replacement are covered. HTTP tests deny upload routes and enforce connect-src/form-action none on /keys. Browser generation/unlock/reload/navigation lock checked at 390×844 and 1440×900, dark mode, no horizontal overflow or console warnings/errors; secret controls have neither names nor form owners and passphrases clear after operations. Screenshots: mailspace-local-keys-mobile.jpg and mailspace-local-keys-desktop.jpg. No mail encryption/MIME or Thunderbird pass is claimed yet.

Container execution is now verified through Podman 5.7.1 and podman-compose 1.5.0 using the actual Dockerfile, with Node 24 / Go 1.27.4 / Alpine 3.23. Personal-mode named-volume startup, non-root read-only root filesystem, health/auth/source archive, session/user/profile/workspace persistence after restart passed. An offline archive of the entire stopped test volume restored into a separate fresh volume/container and preserved the same user/password/profile/timezone. Only synthetic .test users were used; test containers and volumes were removed afterward. The native 8080 preview was untouched. Docker CLI itself is absent; GitHub workflows and cross-platform releases remain unexecuted. Earlier entries recording unavailable container execution describe their earlier milestones.

## MS-006 integrated encrypted-MIME alpha slice

Thirteen Node tests pass, now including the actual vault/MIME methods against isolated GnuPG 2.2.41. Tests distinguish unsigned/unknown-key/invalid/valid-untrusted/valid-trusted signatures; missing mixed recipients, wrong sender key and lock races block output; own Sent/Bcc recipients decrypt while PKESK recipient IDs are wildcarded. MIME size/depth/width limits reject excessive input. Go integration covers ciphertext JSON (unknown body fields rejected), Origin/CSRF, two-user compose/raw/read/send denial, bounded MIME reconstruction, no plaintext fallback, idempotent SMTP and one Sent copy, and original raw download identity checks.

Real browser synthetic round-trip on an isolated 18081 sandbox: generate/verify own key → unlock → sign/encrypt → local TLS SMTP → accepted/saved Sent → refresh → local decrypt → valid verified signature. Script markup appears as literal text. 390×844 dark reader has no overflow; 1440×900 desktop checked; no console warnings/errors. No forms or named/form-owned secret controls; passphrase clears. Lock empties/hides decrypted DOM. SQLite DB/WAL files contain neither private body canary, test key passphrase nor private-key armor after send/decrypt. Screenshots: mailspace-openpgp-reader-mobile.jpg and mailspace-openpgp-reader-desktop.jpg. No real recipient contacted. Full make check passed; the final template-only additions receive another build/render gate before commit. Thunderbird/detached-signature/binary legacy variants are explicitly still pending; global openpgp remains false.

Additional actual-handler egress regression: a minimal DOM drives the shipped composer and reader event handlers with real local keys. A captured POST contains only allowlisted ciphertext/metadata; body/passphrase/private-key canaries are absent. Local decrypt makes exactly one GET for original ciphertext and no upload; locking clears text. Final make check passed after reply locator/template additions, and the new handler regression passed independently.

## MS-006 local draft/attachment slice

Sixteen Node tests pass, including ciphertext-only draft records after close/reopen, user/account separation, transplanted scope rejection, limits/overwrite safety, original nonce preservation and explicit removal. Actual handler egress captures include plaintext file/filename canaries (absent from POST), and initialization failure leaves unlock/compose disabled. GnuPG round-trip now includes binary attachment MIME. Unicode filenames and exact binary octets survive local encryption/decryption; malformed path/control names and oversized/count selections fail before upload (oversized files before reading).

Real isolated browser sandbox: generate/verify own key, select synthetic UTF-8 filename, save with an as-yet-untrusted recipient, reload→unlock→load restores body/file; sending to that missing key is blocked. Change to the synthetic own recipient→send→accepted/saved Sent→refresh→decrypt with valid verified signature. Native Blob download compares byte-for-byte to the original file. The confirmed queued draft disappears from the local list. DB/WAL scans contain none of the private draft/file/filename/passphrase/private-key canaries. Mobile 390×844 dark composer has documentWidth=390, zero forms; desktop reader checked, no console warnings/errors. Synthetic download removed after comparison. Screenshots: mailspace-encrypted-draft-mobile.jpg and mailspace-encrypted-attachment-desktop.jpg. The QA sandbox is temporarily retained for further compatibility work; no actual mailbox/provider contacted. Session expiry and idle lock required reauthentication during this long-running check and were not bypassed.

Final draft/attachment gate: make check passed (asset build, formatting, 16 Node tests, Go vet/race and actionlint). The synthetic file download was compared through the browser’s native download event, then removed. No global software was installed; Thunderbird/rnp are not available locally and that compatibility gate remains open.

## Text-first MIME and HTML preview (MS-008 slice)

Go tests cover HTML-only Unicode/entity conversion, native plain preference, encrypted-sibling suppression and bounds. Nineteen Node tests include a malicious HTML corpus, text conversion and the empty-sandbox source-document lifecycle. HTTP tests cover two-user ordinary-reader denial, image/frame CSP and raw-source authorization. Dependencies are pinned: x/net 0.62.0 (BSD), DOMPurify 3.4.17 (Apache option), jsdom 31.0.1 dev-only. Host Node 26.12.1 and the existing container Node 24.22.3 satisfy the test engine requirement.

Real browser: ordinary HTML-only is readable as text without a private key; explicit Preview HTML shows headings/table in an isolated source document. Escape removes the frame. An encrypted HTML fixture decrypts/converts locally and opens the same preview; unknown signer remains explicitly unverified. A loopback HTTP counter for malicious script/image/CSS/frame/form/link/refresh traps stayed at zero through visible ordinary and decrypted previews. Private HTML canary is absent from SQLite DB/WAL. Desktop and 390×844 dark modal checked with documentWidth=390. Screenshots: mailspace-html-preview-desktop.jpg and mailspace-html-preview-mobile.jpg. Only synthetic .test messages and local probe were used. Local Blob frames did not render reliably in the in-app browser; the verified implementation uses the reviewed sandbox srcdoc boundary instead, preserving parent frame-ancestors none and exact fixed-style hash. No production-ready full-HTML or remote image support is claimed.

Final HTML gate: make check passed with 19 Node tests, Go vet/race and actionlint; govulncheck returned No vulnerabilities found. The encrypted HTML modal was also automatically closed/removed by idle lock (frames=0, decrypted content cleared). The local resource probe and synthetic public-key download were removed/stopped afterward.

## Message-level Space/source foundation (MS-020, ADR-0017)

Full make check passed: locked assets/formatting, 19 Node crypto/HTML tests, Go vet and all race tests including new collection/timeline suites, plus actionlint. Tests prove separate membership for messages from one original thread, multi-Space membership, independent labels, human revision/idempotency, owner/organization isolation, optimistic description updates and atomic rollback. Source tests cover exact-copy dedup, conflicting/missing IDs, current account revision/UIDVALIDITY, immediate Outbox state and later Sent dedup. The account-save result returns its committed revision. v1→v6 upgrade and newer-schema refusal pass. This is a backend foundation, not a claim that the new UI or a decision provider is already connected.

## Product root, dialog and deployable personal installation (ADR-0019)

Full make check passed: local asset/format checks, all 20 client crypto/HTML/egress tests, Go vet, all Go race suites and actionlint. New regressions cover operator password/session atomic rollback and isolation, original thread cycles/ambiguous IDs/account revisions, root/legacy redirects, original thread replies and foreign-source denial, dialog author/timezone runs and on-demand assignment/native fallback. Retired UI tests were removed with their retired implementation; MIME/reference helpers still used by the transport API remain covered.

Native browser: original five-message cached thread opens from a message; Reply to its first source selects the original Maya recipient, submits to the isolated TLS fixture and appears as an own outgoing sixth message. Back-to-Space and thread context links work. On-demand Space/label choices open inline. Desktop, 390×844 and 390×568 checked: document dimensions match viewport, no horizontal/document overflow and no console errors. Light/system and dark reviewed; keyboard Enter navigation checked. Screenshots: mailspace-thread-dialog-desktop.jpg and mailspace-thread-dialog-mobile.jpg.

Stateful deployment acceptance ran through the installed Podman-compatible Compose provider (Docker Engine is not installed on this host). The final image built successfully. A real local TLS reverse proxy with explicitly trusted synthetic CA exercised Secure/HttpOnly/SameSite login, Space creation, provider setup with verified TLS IMAP/SMTP, real sync and SMTP acceptance. Restart retained login, Space, credentials/cache/outbox. Offline tar included DB plus credential keyring; fresh named-volume restore recovered the original password/data and provider access. Operator reset revoked prior sessions; post-restore revocation prevented session resurrection. Corresponding source downloaded from the image without operator .env/backups. No real provider/user mailbox, public DNS/certificate or external model was used; no global trust settings were changed. Caddy is a configuration example, not an executed public-server deployment. QA containers/volumes are removed separately after checks.

The server runbook and readiness checklist are shipped with the source. No GitHub CI run, public release/GHCR publication, Thunderbird matrix, team ACL or independent security-audit pass is implied.

## Optional classification and backup testing — 2026-11-13

Published testing snapshot 1cd317a passed GitHub CI. Local `make check` passed all 21 browser tests (including GnuPG interoperability) and all Go race tests. Follow-up HTTP tests exercise missing CSRF/consent, provider change, anonymous access, a second owner with classification enabled, rejected unknown JSON fields, exact selected-excerpt payload, dry-run-only encrypted proposals and explicit owner approval without re-export. Browser lifecycle regression prevents an old response clearing a new excerpt.

Linux Podman build of the optional Litestream 0.5.18 target succeeded. A file replica recovered both an initial user and a later user created after replication started, into a new volume; SQLite integrity_check returned `ok` with two users. No production mail/token/bucket was used. `scripts/check-backup.sh` repeats ongoing-write recovery with unique disposable volumes and runs in CI. S3 credential/routing/access policy and live paid OpenRouter availability remain operator checks, not test-suite claims.

The same authenticated synthetic UI was checked at desktop 1280×720 and mobile 390×844, including independent feed scrolling, light/dark modes, inline accepted synthetic reply and provider-disabled automation/manual mode. Document dimensions matched each viewport; no console errors were observed. README screenshots contain only synthetic .test mail and generic Spaces. The user's port 8080 was not restarted.

Follow-up startup diagnostics name missing model/token/endpoint settings without printing secrets. TypeSafe Jev transport was corrected to the documented typed Decisions API, with contract tests for independent multiple-Space Noul results, conservative confidence, incomplete/foreign/invalid answers and no tool/trace/private-identifying request fields. Ordinary-model chat compatibility remains separate. Live paid Jev/OpenRouter availability is not claimed.

## Live synthetic Jev check — 2026-11-13

Two explicitly authorized, bounded requests to typesafe/jev-1.13 via OpenRouter Decisions succeeded with no retries. A synthetic invoice matched Payments (routing confidence 0.920); a synthetic cinema booking matched Personal (0.880). Inputs contained only made-up .test addressing and generic briefs. No production mail, key content or raw provider response was logged/committed. This validates actual connectivity/contract/privacy routing for that test, not every future provider/model or correctness on private mail. CI skips this opt-in test unless a private operator key path is explicitly supplied.

A real-browser synthetic HTML message rendered in the isolated dialog with script/image payloads absent. An extra Read-as-text click had hidden preview availability; ordinary readers now load text automatically. HTML-only MIME remains converted to inert text, with formatted preview opt-in. The legacy 8080 server remains stopped.

## Local rules (schema 9)

Full `make check` passed: 23 client tests including GnuPG interoperability, asset formatting, Go vet, all Go race suites and actionlint. Rules tests cover disabled creation, read-only previews, idempotent many-to-many assignment, manual veto, stale revision, body exclusion, encrypted/own-message exclusion and two-user denial. HTTP tests cover CSRF, consent, foreign rules/Spaces and no provider calls. Domain validation has an additional regression test.

The disposable browser preview verified create-disabled → preview → explicit enable with synthetic `.test` data, keyboard focus, desktop and 390×844 layout (document width 390). No production mailbox or API key was used. Screenshot saved as `local-rules-mobile.png` in the task outputs. The temporary preview was stopped afterward; port 8080 was not started. No provider-side spam operation or complete 1.0 acceptance is claimed.

## Local detached signing foundation (ADR-0028)

Full `make check` passed with 24 client tests, real GnuPG interoperability, asset/format checks, Go vet/race and actionlint. The application-generated SHA-512 detached signature verifies in GnuPG; modified MIME bytes fail verification. Client tests preserve exact Unicode/attachment MIME, snapshot caller-mutated input, enforce sender binding and lock, and reject noncanonical/8-bit signed entities. Network APIs are forbidden by the client-crypto test harness. No UI or signed-only submission was added; no Thunderbird acceptance is inferred. Main at 2a6afe6 also passed GitHub CI after the direct merge.

## Spaces workflow and attachment UX (ADR-0029, schema 10)

Full make check passed: 26 client/GnuPG tests, format/assets, Go vet/race and actionlint. New tests cover 20,000 distinct cached correspondents with only 50 rendered/page and six sidebar shortcuts; search, archive filtering, 16-color persistence, cross-owner/CSRF archive denial, restore preserving assignments/originals, scoped drawer response/reply IDs, three-message Jev previews with cached reuse and separate saved Auto behavior. Attachment tests enforce inert text, unsupported HTML/SVG/PDF refusal, PNG dimension bounds and removal/revocation lifecycle. Busy-form tests preserve explicit serialized plaintext consent and restore controls after failure. No new paid-provider calls were made for these workflow tests.

Real disposable browser: Show thread stays over the original timeline; reply and direct Space changes stay in the drawer. Attachment discovery exposed a local Blob download and text preview. Cmd/Ctrl K, Arrow Down focus, Escape, People search, local Archive → Archived → Restore and 16 named radio swatches were checked. Desktop and 390×844 drawer/color views had documentWidth=390 and no duplicate DOM IDs. Visual QA found/fixed missing palette build sources; all 16 computed swatch backgrounds differ. Formatted hidden archive whitespace has a regression. Screenshots: thread-drawer.png, thread-drawer-mobile.png and space-colors-mobile.png in task outputs. Native Safari-compatible controls were retained; no separate real-Safari test or PDF/JPEG preview acceptance is claimed. The disposable preview is stopped; 8080 was not started.

Follow-up: main 6b7bec5 passed GitHub CI including Docker and the ongoing SQLite write/restore drill. Saved-mode Jev batches also have a regression proving manual unassignment is skipped without a provider call or aborting the scan; full make check passed again.
