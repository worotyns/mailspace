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


## 2026-11-14: personal workflow testing snapshot 45bd0f1

- Local `make GOTOOLCHAIN=go1.27.4 fmt check` passed: 59 client tests, asset build/notices, formatting, Go vet/race suites and available actionlint. The final mobile-only slice repeated the full check successfully.
- GitHub checks passed for fdb5a52 and 45bd0f1, including Docker Compose configuration/build and the ongoing SQLite-write Litestream restore drill. No stable release/multiarch publication is implied.
- Fresh Dockerfile runtime built through Podman. A disposable named volume and `.test` user passed non-root/read-only startup, health/login, bundled readable-text/Marked assets and upstream notice availability, restart and persisted-user lookup. No host port was exposed; the test container and volume were removed.
- Isolated browser: imported only a synthetic protected key. Inline decrypt rendered the synthetic signed message, verified signature and bounded local attachment; another preview after full reload reused the SharedWorker without another passphrase. Initial IPv6 CSP source mismatch was fixed with the documented same-origin IP-literal policy and regression coverage; authentication failures were never bypassed. Diagnostic edits were removed before commit.
- User-defined header label changes updated badge and sidebar count without swapping the message or composer. Sidebar label navigation resets unrelated primary filters while retaining account scope. Native mobile switcher at 390×844 opened from the title, navigated to another Space and closed on Escape. Root/body scrolling stayed zero after focus/navigation once the app viewport used clipping; native settings/composer document scrolling is unchanged.
- Native Safari acceptance was not executed: automatic approval review refused inspection of an unrelated private Safari document. Testing continued in the isolated in-app browser without reading that document. Real Thunderbird, broad external-provider/charset/large-history acceptance and team ACL remain open; these checks do not constitute an independent security audit or completed 1.0.


### Repeatable runtime acceptance

`scripts/check-runtime.sh` builds and starts the production image against its own synthetic disposable volume without exposing a host port. It checks non-root/read-only execution, health/login, bundled parser/license/source availability, anonymous crypto-session denial and user persistence after restart; trap cleanup removes only its own container/volume. Run `MAILSPACE_CONTAINER_CLI=podman scripts/check-runtime.sh` with Podman or the default Docker command. GitHub CI runs this separately from the Litestream restore drill. It does not use production state or validate live mail-provider delivery.

### Reply source freshness

Outbox preparation now rechecks the resolved original inside the transaction that records the send intent. Deterministic regression tests cover current originals, UIDVALIDITY reset, deleted cached UID, changed outgoing revision and deleted outgoing original between resolution and transaction. Stale sources return a conflict with no child row or SMTP/Sent-copy attempt. Existing foreign-owner and uncertain-outcome behavior remains covered. No signed-only endpoint or new plaintext-export permission is added by this fix.

## Feedback: settings, panel return, thread replies and headers

Full local `make GOTOOLCHAIN=go1.27.4 fmt check` passed (65 client tests, formatting/assets/notices, vet, race suites and actionlint). Native Settings navigation renders a common width/padding/current section; the isolated browser measured the same content x/width across Accounts and Profile at desktop and 390px. The wordmark emphasizes mail only. A thread footer click opened a writable, correctly addressed composer; Encrypt opened the local-key dialog. A synthetic encrypted new message was accepted by the loopback SMTP provider and saved in Sent. Opening/closing its Outbox result left one timeline shell and zero remaining panel frames. No real mailbox or key was used.

The unlock dialog is no longer hidden behind asynchronous key listing; pending/failure/closed-dialog behavior and failed worker loading are regression-tested. This does not yet certify the reported Safari failure as fixed: native Safari reproduction still requires the requested empty test window/version details. Previous automatic review refused reading an unrelated private Safari document; no attempt was made to bypass that restriction.

Header-complete cached threading, explicit missing-ancestor grouping and guarded legacy metadata repair are covered by the focused and full Go suites (ADR-0035). Subject labels normalize Re:/Odp: for presentation; subject similarity alone still does not join messages.

SharedWorker-startup compatibility follow-up passed full `make check` with 76 client tests plus Go vet/race/format/assets checks. Recovery is limited to one metadata listing retry in a fresh authenticated dedicated worker. The regression matrix rejects recovery after any response, private-operation attempt, authentication error, messageerror, timeout or lock, and covers stale unlocked state, original deadlines and overlapping keeper generations. Internal read-only review caught and corrected two lifecycle races before publication. Native Safari acceptance remains unperformed, pending the requested isolated window; passing simulated transport failures is not claimed as a Safari runtime pass.

## MS-020 inline reply refresh and compact thread feedback — 2026-11-15

Implemented explicit scoped cancel targets (overriding the enclosing reply form's status-only HTMX target), recorded plain-send refresh, duplicate-settle suppression, and deferred underlying-timeline refresh after a thread send. Validation errors retain the draft; accepted/unknown/rejected outcomes retain the existing single-attempt semantics. Thread presentation removes classification/Space/label controls, keeps a direct original-message Reply and SMTP state, and uses the panel surface behind the sticky date separator. Below the small breakpoint, message rows no longer reserve an avatar column or an extra percentage width margin.

Final asset/notices build and formatting passed. Client suite: 77/78 passed, including the new refresh/scope regressions; the GnuPG interoperability test failed because the sandbox could not connect to gpg-agent (IPC connect call failed), so full `make GOTOOLCHAIN=go1.27.4 check` did not pass. `go vet ./...` passed with a writable temporary GOCACHE. Separate `go test -race ./...` passed webui and the other reported packages except httpapi, whose synthetic mail test was denied a loopback listener (`bind: operation not permitted`). A separate race run of httpapi excluding only that blocked listener test passed (33.5s). No workflow was changed; actionlint is unavailable.

Live browser acceptance at phone/desktop/short viewport and keyboard interaction is unperformed: the disposable dev sandbox was denied its synthetic IMAP listener. Docker is unavailable; Podman Compose was also blocked from its local VM socket, so no container config/build/smoke pass is claimed. `git diff --check` passed. Git staging was denied creation of `.git/index.lock`; this execution cannot commit or push. No production server, mailbox, provider token or key was used. Signed-only submission, real Thunderbird/Safari, team ACLs and remaining 1.0 release acceptance remain open.


### MS-020 completed acceptance after approved local access

The follow-up at fcd29fc passed `make GOTOOLCHAIN=go1.27.4 check`: all 78 client tests including GnuPG, asset/notices and formatting checks, Go vet and the full race suite. The integration assertion now checks the selected original-message reply URL instead of the removed Show thread control. `gofmt` and `git diff --check` passed. Actionlint is unavailable; no workflows changed.

A fresh disposable synthetic sandbox and isolated Chrome verified keyboard Enter cancellation in both feed and drawer, Escape dismissal, scoped fragments and duplicate-ID absence at 390×844, 1440×1000 and 1280×500. All three retained one viewport with zero root/body scroll and no horizontal overflow; the phone avatar gutter was absent. Visual screenshots were reviewed. Two explicit synthetic plain sends verified automatic feed refresh, scoped thread refresh and underlying-feed refresh on thread close, without a reload or JavaScript errors. No real mailbox or private key was used.

Docker is unavailable. Equivalent `podman compose config --quiet` and `MAILSPACE_CONTAINER_CLI=podman scripts/check-runtime.sh` passed: image build, non-root/read-only runtime, health/login, local assets/source/notices, anonymous crypto-session denial and persisted user after restart. Only the script's disposable container/volume were removed. The earlier restricted-run failures above describe the initial environment, not the final acceptance result. Native Safari worker acceptance is recorded separately in ADR-0036; Thunderbird/team/security/release gates remain open.


## MS-006 authored signed transport — ADR-0037

The dedicated signed JSON contract and canonical MIME boundary passed full `make GOTOOLCHAIN=go1.27.4 check` (78 client/GnuPG tests, assets/upstream notices, formatting, Go vet and full race suite), gofmt and diff review. Existing encrypted and ordinary payload/idempotency behavior remains covered. New cases cover explicit plaintext consent, authored/unknown/client-decrypted provenance, mixed/unknown JSON fields, owner/CSRF denial, encrypted-parent refusal, stale-source transaction races, unknown SMTP outcomes without retry and unchanged legacy nonce JSON hash input. The parser fuzz run passed 109,395 executions in 11 seconds, including empty/overlapping MIME boundary seeds.

A LocalVault/composeEntity/signedEntity-generated synthetic Unicode/binary-attachment fixture traversed HTTP, durable outbox, synthetic TLS SMTP and Sent APPEND with exact byte identity. An isolated short-path GnuPG home verified the resulting entity and rejected a changed signed byte. The public fixture key is checked in; no private key is retained. The test initially exposed a long temporary Unix socket path and a test mutation of shared fixture bytes; both test harness issues were corrected before the final full pass.

The final production image passed the disposable Podman runtime smoke (build, non-root/read-only, health/login, bundled assets/source/notices, anonymous API denial, persistent user after restart). Docker and actionlint are unavailable; no workflow was changed. This slice adds no visible send control and does not claim full signed-only UX or Thunderbird acceptance. No real mailbox/provider/key, paid model, release or deployment was used.


## MS-006 full-composer signed-only action — ADR-0037

Final `make GOTOOLCHAIN=go1.27.4 check` passed: 79/79 client tests including GnuPG, assets/upstream notices, formatting, Go vet and the full race suite. Gofmt and diff review passed. The actual shipped composer event test verifies explicit per-send readable-content consent, missing-consent focus and retained draft, encrypted-original refusal, no recipient-key requirement, exact payload allowlist/signature/Unicode/binary attachment bytes, no passphrase/private key upload and one attempt after an ambiguous connection loss. Lock clears the body/consent and resets to encryption. HTTP regressions cover the disabled signed-only option for an encrypted original and preservation of the original Sent reply source when switching composer protection.

Isolated Chrome on a fresh synthetic profile exercised Space/All activity → selected Reply → Expand → local key generation/trust/unlock → Sign only. Keyboard Space selected protection and consent; missing consent blocked POST and focused its checkbox. At 1440×1000, 390×844 and 1280×500, there was no horizontal overflow or duplicate ID; full-page screenshots were reviewed (native composer document scrolling is intentional). An explicitly approved signed reply with Unicode and five binary octets reached the loopback SMTP server. Its accepted outbox raw MIME was identical to the submitted signed entity, and a local public-key verification reported the trusted sender and exact text/attachment bytes. No browser errors or secret-bearing signed request were observed. Send cleanup cleared the body and consent and restored encryption.

The final Podman production-image smoke passed on disposable data: non-root/read-only startup, health/login, local assets/source/notices, anonymous API denial and persisted user after restart. Docker/actionlint remain unavailable; no workflows changed. Signing-only in native Safari and real Thunderbird are not claimed by these Chrome/GnuPG tests. No real mailbox, key, paid provider, release publication or deployment was used.


## MS-006 explicit local quotes and protected draft payload v2 — ADR-0038

Final `make GOTOOLCHAIN=go1.27.4 check` passed: 82/82 client tests, GnuPG, asset/upstream-notice build, formatting, Go vet and the full race suite. `git diff --check` passed; no Go source changed. New behavior tests cover ordinary/encrypted/HTML-only text, Unicode, unknown/untrusted/verified signatures and invalid-signature refusal, 32 KiB bounds, empty originals and no attachment copying. The shipped composer handler makes only the fixed exact-source raw GET, retains author text after missing-source failure, suppresses duplicate quote insertion, clears/aborts on Lock and ignores a late response. Decrypted quotes block signed-only sending, remain protected after explicit ciphertext draft save/load and leave only inside an encrypted send with the existing nonce/context; ambiguous submission never retries. DOM attributes, non-body controls and request payloads are checked for plaintext/key/passphrase canaries.

Draft-format regressions cover authenticated v2 flags, v1 new-message versus conservative v1 reply loading, unknown versions/flags, malformed locators, account/From scope and a genuine foreign-signed encrypted draft injected with edited unencrypted fingerprint metadata. Only the unlocked personal signing key can authorize a self-draft. New v2 drafts remain ciphertext in the unchanged IndexedDB namespace; rollback requires returning to a v2-capable build to read them.

The final disposable Podman production-image build/runtime smoke passed (non-root/read-only, health/login, bundled assets/source/notices, anonymous API denial, persisted user after restart). Docker/actionlint remain unavailable; no workflow changed. Per the owner's request, manual mobile/desktop/keyboard and Thunderbird acceptance are left for their later check and are not reported as passed. No real mailbox, provider, key or model was used; no release/deployment was made.


## MS-020 compact local previews and visible send state — ADR-0039

Full `make GOTOOLCHAIN=go1.27.4 check` passed: 83/83 client tests, GnuPG, asset/upstream notices, formatting, Go vet and the full race suite; gofmt and diff checks passed. Attachment cases verify compact labelled SVG controls, request-only inline CID/PNG/JPEG creation, dimensions/size and malformed/unsupported input refusal, no attachment HTML execution, Blob URL revocation and stale callbacks after hide/clear. Text previews still use inert textContent in a native dialog. The real shipped reader/composer egress tests remain green with the shared local Tabler sprite; no mail SVG is rendered.

Render regressions prove one accepted/pending/unknown/rejected send state appears in the message header before the scrolling Space toolbar and retains honest server-acceptance wording. Settings now says Decision Model while routes/provider environment identifiers stay compatible. HTML and attachment previews use compact labelled close icons; local attachment download/preview actions use download/eye icons. Images stay outside the sanitized HTML iframe and remote image loading remains blocked, matching the owner's explicit local/CID choice.

The disposable Podman production image build/runtime smoke passed again. Docker/actionlint are unavailable; no workflows changed. Manual mobile/desktop/keyboard acceptance is left to the owner on request, not reported as passed. Recipient/Cc/Bcc/datalist feedback and newly requested Authenticator App 2FA (MS-021) remain active work; this slice does not claim those functions are complete.


## MS-021 identity/store foundation — ADR-0040, schema 13

Full `make GOTOOLCHAIN=go1.27.4 check` passed: 83 client/GnuPG tests, asset/upstream notices, formatting, Go vet and full race suite. The expanded identity race package passed in the full run (173.9s under concurrent package load). New tests cover RFC 6238 SHA-1 six-digit vectors, adjacent/excessive clock skew, encrypted user-bound seed persistence, session/password enrollment boundaries, setup expiry/restart, no session from password alone, challenge CSRF/replay, cross-challenge TOTP/recovery races, shared persistent budgets, password-reset/revoke/version races, atomic session-failure rollback, code-set regeneration and explicit recovery when auth keys are lost. Ciphertext transplantation across users is refused. Lost keyrings are not recreated by another enrollment. Existing/future-schema migration and upgrade tests passed after accounting for schema 13.

Pinned pquerna/otp v1.5.0 is Apache-2.0; its runtime QR dependency boombuler/barcode is MIT. Source imports were inspected for runtime network/process hooks; none was found in the shipped packages. No lifecycle/generate command was invoked. Generated Go notices include both libraries and OTP's NOTICE; the existing notice gate was not bypassed. No vulnerability-audit claim follows from this license/source review.

The disposable Podman production-image build/runtime smoke passed, including license/source assets, non-root/read-only behavior, health/login, anonymous API denial and persisted user after restart. Gofmt/diff checks passed; Docker/actionlint remain unavailable. This commit delivers the tested identity foundation only: HTTP login/Settings/QR/CLI integration and its acceptance remain pending, with MFA still unadvertised. No real account, app seed, key, paid provider, release or deployment was used.


## MS-021 integrated Authenticator App flow — ADR-0040

The integrated Security/login/operator slice passes `make GOTOOLCHAIN=go1.27.4 check`: local asset/license build, formatting, 87 client/GnuPG tests, Go vet and the full race suite. Focused HTTP and operator tests use only synthetic `.test` users and temporary databases/keyrings. They cover password/Origin/CSRF enrollment, owner and exact-session isolation for QR/seed, local bounded PNG, invalid and consumed app codes, first-code activation, once-only recovery display, session rotation and revocation, pending-cookie denial (including substituting its value into the full cookie), app/recovery login, challenge cancellation/replay, shared budgets across new challenges, explicit recovery after auth-key loss without regenerating the keyring, native recovery regeneration/disable and local operator reset preserving other users/passwords. Bounded form inputs fail closed. Browser-side tests verify radio/focus behavior, zero storage/egress, clearing sensitive DOM and preserving password/code serialization during the lock broadcast from native submission.

The initial integration used a full native Security page to avoid stale parent CSRF; the subsequent Security modal fix below supersedes that navigation choice. Render checks cover shared settings navigation. All pages remain no-store, hx-disable/hx-history=false, local assets and native form actions. No workflow was changed; actionlint and Docker are unavailable. Compose configuration and a disposable Podman production-image build/runtime smoke passed (health/login, local assets/notices/source, anonymous API denial, non-root/read-only startup and persisted user after restart).

Manual pairing with a real Authenticator app, mobile/desktop/low-window presentation and keyboard acceptance are deferred to the owner as requested; they are not reported as passed. No real mailbox, production credential, external QR service, release or deployment was used. Recipient metadata/Cc/Bcc/datalist follow-up and the wider 1.0 gates remain open.


## MS-020 inline To/Cc/Bcc follow-up

`make GOTOOLCHAIN=go1.27.4 check` passed: local asset/notices build, formatting, 89 client/GnuPG tests, Go vet and the full race suite. Client acceptance encrypts to multiple To/Cc/Bcc keys and successfully decrypts as each recipient; an untrusted Cc or Bcc blocks before any POST and keeps the draft. Additional cases cover combined count, byte/address bounds, header/control injection, empty recipients, Bcc-only submission, frozen account/routing across async encryption, ciphertext-only egress and no second attempt. Render tests preserve the exact selected-original recipient without attribute whitespace and verify independent scoped labels/datalists and native form fallback.

The isolated TLS HTTP→SMTP→Sent test passed with multiple To, Cc and Bcc, authoritative reply references, owner/CSRF checks, nonce replay and private Bcc envelopes without a Bcc header/address in MIME. Native Bcc-only sending and encrypted To/Cc header transport also pass. Compose configuration and a disposable Podman production build/runtime smoke passed. Docker/actionlint remain unavailable; no workflows changed. Manual phone/desktop/keyboard acceptance is deferred to the owner as requested. Recipient metadata display and People suggestion reliability remain open; no release/deployment or real-mail test was performed.


## MS-021 native submit hotfix

Reproduced the reported empty-password request in real headless Chrome against an isolated synthetic installation: before the fix, the native setup POST had an empty password and no QR; after the fix it carries the unchanged password and reaches the local QR. Only boolean presence/match results were recorded, never the password or seed. Native event dispatch can execute a microtask checkpoint between capture and bubble callbacks; the submit guard now lasts through the entire task instead of expiring in a microtask. A regression test emulates that checkpoint and still verifies later foreign locks/pagehide clear sensitive fields. Wrong setup-password feedback now asks for the current Mailspace sign-in password, without requesting a code before enrollment.

Full make check passed (89 client/GnuPG tests, formatting/assets/notices, Go vet and race suites), as did the disposable Podman production build/runtime smoke. Docker/actionlint remain unavailable. This verifies Chrome native submission, not Safari or real Authenticator pairing. Security modal navigation is the immediately following fix; no production session/cookie or user-provided curl was used.


## MS-021 Security remains in the settings panel

Security navigation now replaces the trusted settings frame, preserving the parent URL/timeline. Only its native document/form-response paths enter the app-panel framing exception; QR/unknown paths remain excluded. Successful factor changes send a boolean notification from the current same-origin frame. The parent fetches its own session metadata, validates user/token/expiry, updates CSRF/expiry only and preserves unsent ordinary controls. Unit/HTTP tests cover framing, success-only notification, absence of secrets in cross-frame messages, foreign/stale notices and session responses, pending/failed mutation guards, retry without resending mail, and pagehide/native fallback behavior.

The automated real-Chrome synthetic flow passed: Accounts→Security stayed in the modal, a wrong password had stage-specific feedback, correct password reached local QR, an app code generated with the pinned TOTP library activated MFA, ten recovery codes were shown, explicit recovery proof disabled it, and both rotations refreshed the parent's CSRF without replacing its DOM or losing the unsent authored draft. The top-level URL did not change. Layout checks at 1440×1000, 390×844 and 1280×500 kept the modal open without document overflow. No production credentials, real mailbox, external QR service or real Authenticator app were used; Safari/real-app and owner manual acceptance remain open.

Full make check passed with 95 client/GnuPG tests, assets/notices/format, Go vet and race checks. Disposable Podman production-image runtime smoke passed; Docker/actionlint remain unavailable. No release or deployment was performed.


## MS-022 compact label popovers and contact addresses

Real-Chrome synthetic acceptance passed at 1440×1000, 390×844 and 1280×500: label choices open as a compact native top-layer popover within viewport bounds; Enter/Escape and focus return work. A label toggle updates only its metadata, reopens/focuses the picker, and preserves the exact message body and authored draft nodes. Secondary email text appears under named contacts in People and sender headers. There were no JavaScript/CSP failures in this flow. Native details/POST remains the fallback when Popover support is absent. Unit tests also cover the focus-induced scroll regression (opening must not immediately dismiss), user scrolling outside, and scoped metadata-only swaps.

Full make check passed with 99 client/GnuPG tests, format/assets/notices, Go vet and race suites. Disposable Podman production build/runtime smoke passed; Docker/actionlint remain unavailable. This is automated Chromium acceptance with synthetic mail; Safari and owner manual acceptance remain separate. Automatic provider read/unread and upward history pagination are recorded as still open in MS-022, not claimed implemented.


## MS-022 cached-history pagination — ADR-0041

Targeted checks passed: history/UI Go tests, the isolated HTTP→cached-history/authorization integration (55 added synthetic rows), selected webui race tests, Go vet, gofmt/diff checks, local asset/upstream notice build, and nine client history/inline-reader tests. Coverage includes latest-20 and stable older boundaries despite newer arrivals, per-user/account/filter/timezone scope, invalid/unknown/stale cursor rejection, native fallback, 20-row fragments without composer/root replacement, selected-original visibility, full-view counts, no flag mutation, GET-only/no-form paging, deduplication retaining existing nodes, keyboard focus, scroll correction, failure/retry/cancellation and adding encrypted entries without clearing older previews.

Real headless Chrome passed at 1440×1000, 390×844 and 1280×500: initial 20 at the newest/bottom, upward scroll loading 20 more, no duplicate keys, unchanged retained message/body/composer nodes, retained authored draft, stable viewport anchor and no document overflow or JavaScript error. Synthetic cached rows only; no production mailbox, credentials, mutation or deployment was used. The existing per-folder/outbox/1,000-entry cache limits and 100-message thread limit remain. Native Safari/manual production acceptance remains open.

The owner explicitly requested reducing/skipping broad tests to save work. Accordingly the full make check, repository-wide race suite and container build/runtime smoke were **not repeated** for this paging slice; their earlier passing results belong to earlier commits. Do not infer that all 1.0/release gates have passed.


## MS-022 compact Space picker — ADR-0042

The always-expanded horizontal Space chips are replaced by the shared compact native popover already used for labels: assigned Space badges in the summary, every authorized Space as an independent toggle in the popover, and an out-of-band sidebar count refresh. In All activity/Unread/Unsorted every assigned Space is badged; inside one Space only the other assigned Spaces are badged, while the popover still offers the viewed Space for explicit removal. The mutation remains the owner-scoped, CSRF-protected membership POST and replaces only the picker plus sidebar counts, so message bodies, browser-only decrypted content and unsent drafts stay mounted.

Automated evidence: `make check` passed with 103 client/GnuPG tests, formatting, local asset/upstream-notice build, Go vet and the full race suite. Go tests cover assigned-badge scope in All versus one Space, preserved many-to-many `NextSelection` toggles, scoped `spaces-message-*` IDs, the OOB `sidebar-spaces` fragment, foreign-account 404 and missing-CSRF 403 denial, and the absence of any private preview in the response. Client tests cover both pickers, the native `details`/form fallback without Popover support, popover reopen with focus on the changed badge after an HTMX swap, and the focus-induced scroll regression. Both pickers share one reviewed module; no message text, key or passphrase enters the popover or the request.

Not covered: a real headless-Chrome/keyboard/mobile pass for this slice and owner manual acceptance, which remain open. Docker/actionlint remain unavailable. No provider folder, mail flag, model call, schema or storage change was introduced.


## MS-022 message header identity line

Each started message run now shows one identity pair on two lines instead of repeating a display name. Received mail shows `sender name -> account name` on the first line and `sender address  account address` on the muted second line. Mail this user sent shows `You -> recipient summary` (first non-account `To` address, `+N` when several) with `account address  first recipient address` beneath it. A Unicode arrow marks the direction and the meta row stays vertically centred; the accessible article label distinguishes sent from received. Because the timeline, Spaces, label/person/query filters and the original-thread drawer share one message template, the change applies to all of them consistently. If an account is named after the same person, the name appears on both sides of the arrow; renaming the account to a mailbox label avoids that.

Automated evidence: `make check` passed with 103 client/GnuPG tests, formatting, local asset/upstream-notice build, Go vet and the full race suite. A Go render test covers the received sender name to account name with both addresses, the sent `You -> recipient +1` summary with the account and recipient addresses, the Unicode arrow and the accessible label. Recipient deduplication and account-address exclusion are unit-tested. Cache previews keep `To` addresses only, so no recipient display name is invented and hidden Bcc recipients stay absent.

Not covered: a real headless-Chrome/keyboard/mobile pass and owner manual acceptance of the new header, which remain open. No provider request, model call, schema, storage or export change was introduced.


## MS-022 provider-synchronized read state — ADR-0043

Read state is the one provider write outside sending. `mailnet.SetSeen` is a separate transport boundary that selects one mailbox read-write, requires the caller's exact UIDVALIDITY, stores only `\Seen` on one UID and confirms the server's returned flags before reporting success. `mailsync.SetSeen` authorizes the owner/account, requires the exact cached account/mailbox/UIDVALIDITY/UID/connection-revision row with a downloaded body, rechecks the revision during the remote call, and only then updates that one message's `flags` in a transaction that rechecks the revision again. `POST /messages/seen` requires Origin/CSRF and an explicit `seen=0|1`, accepts IMAP references only, and uses a dedicated per-user rate window plus two concurrency slots instead of the send budget. The HTMX response replaces only the `seen-*` fragment; the native form fallback redirects. Automatic marking happens only on an explicit content open: the ordinary reader on load, the encrypted reader after a successful local decrypt, and an inline decrypted preview after it becomes visible. Fetching, paging or refreshing a timeline performs no write.

Automated evidence: `make check` passed with the expanded client suite, formatting, local asset/upstream-notice build, Go vet and the full race suite. Transport tests against the isolated TLS fixture cover applied/reverted `\Seen`, stale UIDVALIDITY, unknown UID, unknown mailbox and unchanged provider state after a rejected write. The HTTP integration test covers foreign-account 404, missing-CSRF 403, ambiguous state 400, the scoped fragment without private preview, cached read/unread state after confirmation, the silent 204 background confirmation, a page fetch that leaves flags unchanged, stale revision and unknown locator fail-closed, and a final provider snapshot with no leftover `\Seen`. Client tests cover exactly-once automatic marking, disabled/missing/already-read no-ops, retry after a failed write, and the inline scoped form. `MAILSPACE_IMAP_FLAG_WRITES=0` disables the capability and hides the toggle.

Not covered: a real headless-Chrome/keyboard/mobile pass, real provider flag semantics beyond the synthetic fixture, and owner manual acceptance remain open. Docker/actionlint remain unavailable. No schema, storage-format or generic decrypted-export change was introduced.
