# Roadmapa i bramki odbioru

Bez obietnic terminów przed pierwszym rzeczywistym pionowym wycinkiem. Kolejność ważniejsza niż daty; nie otwieramy wszystkich etapów naraz.

| Etap | Zakres | Kryterium odbioru / zależności |
|---|---|---|
| M0 Foundation | Repo, AGPL, ADR, threat model, UI fixtures, API, CI, Docker i release | Go checks i UI build; Compose smoke w środowisku Docker; żadna atrapa nie jest oznaczona jako funkcja. Foundation gotowe lokalnie; Docker/CI pending |
| M1 Mail core alpha | Users/sessions, account ownership, credential store, SQLite, read-only sync, SMTP outbox, MIME | Dwóch użytkowników nie widzi swoich danych; testowy IMAP/SMTP TLS; reconnect/UIDVALIDITY/flags; send result ambiguity; bounded MIME; backup/restore. Bez publicznego release dla prawdziwych danych |
| M2 Secure MVP | Client vault, OpenPGP/MIME read/send/sign/verify, conversational i classic UX, attachments, IMAP basic search | Interop GnuPG/Thunderbird, wszystkie recipient keys, zero decrypt egress/logs; drafts/replies ciphertext; threat review i brak cichego downgrade. M1 + crypto wymagane do nazwania produktu MVP |
| M3 Team | Workspace/users/roles, shared mailboxes, thread shares, private/team tags, assignments | ACL również attachments/jobs/live events; revoke i audit; encrypted share nie przekazuje private keys; send-as osobno; migration test |
| M4 Drive + FTS | Wirtualny widok załączników IMAP, opcjonalny cache; opt-in full-text zwykłej poczty; local decrypted search | Bez duplikowania storage domyślnie; provenance/ACL; purge po wyłączeniu; off nie indeksuje; metadata search bez modelu |
| M5 Deterministic decisions | Wersjonowane reguły, dry-run, trace, replay, kontrolowane actions | Powtarzalność tych samych input/rules/context; brak efektów dry-run/replay; idempotency i jawne apply; encrypted content rules tylko w kliencie |
| M6 Intelligence | Semantic search i AI compose/summarize, provider lokalny i opcjonalny zewnętrzny | Każdy moduł osobno off; zwykła poczta bez modelu; widoczny payload/provider; brak cichego decrypt upload; prompt injection nie steruje wysyłką |
| M7 Packaging | PostgreSQL adapter, upgrade tooling, YunoHost package, tłumaczenia i accessibility hardening | SQLite/PostgreSQL kontrakty + migracje; instalacja/upgrade/backup/restore/uninstall YunoHost w VM; clean install bez AI |

## Polityka zakresu

Team i tagi są pełnoprawnym kierunkiem, ale nie opóźniają osobistego secure MVP. Attachment Drive nie jest osobnym serwerem plików na start. Full-text ≠ semantic search: osobne zgody, koszty, zależności, czyszczenie danych. Brak publicznego plugin API; rozszerzenia przez contribution review.

## Najbliższa kolejność prac

1. MS-001 ukończone lokalnie: identity/session foundation + SQLite migrations + authorization tests.
2. MS-002 ukończone lokalnie: encrypted account credentials + endpoint policy/TLS + test-server profile.
3. MS-003 ukończone lokalnie jako bounded read-only alpha: trwałe checkpointy, polling i realne wiadomości w UI. MS-004 SMTP/MIME ukończone lokalnie. Następne MS-005/MS-006 client OpenPGP; starsza historia/native IDLE w MS-018.

Równolegle koncepcyjnie MS-005 (wybór biblioteki crypto), ale nie wprowadzamy nieprzejrzanych zależności i gotowej kryptografii na skróty.

## UI feedback delivered

MS-019 adds responsive pane scrolling, System/Light/Dark, a mail-date Calendar, functional profile/timezone and organization selection. MS-009 organization foundation was brought forward, with private-mail ownership unchanged. Full local review: make dev-sandbox. Next core gate remains browser OpenPGP; sharing/Space autofilters follow the explicit relevance-versus-permission contract in SPACES.md.

MS-005 ukończone lokalnie: zaszyfrowany sejf kluczy w przeglądarce, import/export/lock i jawne zaufanie fingerprintów (ADR-0012). GnuPG round-trip sprawdzony; OpenPGP/MIME i Thunderbird nadal MS-006. Kontener oraz trwałość po restarcie zweryfikowane przez Podman-compatible Compose, bez publikacji obrazu.

MS-006 in progress: zintegrowana alpha podpisanego multipart/encrypted, lokalny odczyt i opaque outbox działają w syntetycznym end-to-end. Lokalne zaszyfrowane szkice i załączniki również działają (ADR-0014). Pozostała macierz kompatybilności, detached signatures, secure quotes i Thunderbird; nie zamykamy jeszcze bramki M2.

## Updated product direction (ADR-0016)

Before expanding optional modules, deliver message-level many-to-many Spaces, manual Unsorted triage, one timeline with People/labels/date jump, and inline replies with a small encryption modal. Original email threads remain transport references. JEV/OpenRouter can classify against Space descriptions after an explicit provider contract; no provider is required. This supersedes the old conversation-only Spaces and three peer view tabs as the main UX.

## Current product entrypoint

ADR-0019 supersedes old UI/default-demo descriptions above: one authenticated Space dialog at `/`, original cached email-thread preview/reply, no Classic/Conversation/Calendar pages or unauthenticated UI demo. Stateful Compose is the default; SERVER_SETUP.md is the operator runbook. MS-020 personal UX is implemented. RELEASE_READINESS.md separates working controlled-test scope from the remaining 1.0 gates.

### Current usability follow-up

Completed slices: container palette inclusion/build guard; specific Jev failure categories; native quote folding. In the compact-controls slice: guided sender/recipient key setup, contact prefill and bounded suggestions, date/range filter, trusted settings/compose panels, local archive/label bulk previews, saved-decision acceptance and metadata indexes. See ADR-0030 for exact limits.

Delivered: shared AND/OR filters/rules, memory-only worker lifecycle and inline decrypt/autopreview. Still required before claiming the requested full UX/1.0 scope: signed-only sending, team-source/send-as ACLs, broader provider/browser compatibility and deployment acceptance.

Local AND/OR filters and rule drafts now share a bounded evaluator (ADR-0031/schema 12); worker sessions and inline decrypt/autopreview are implemented in ADR-0032; signed-only sending and the external-client compatibility matrix remain open.

Labels now provide sidebar quick filters and message-header badges with an inline multi-label chooser (ADR-0033). Membership updates replace only label metadata, preserving inline decrypted previews and composer state.

### Current interaction polish

- Labels are user-defined private metadata: sidebar quick filters and per-message header toggles (ADR-0033). No built-in Todo/Done statuses or implicit exclusivity.
- Memory-only worker sessions and inline local decryption, with explicit lock and optional per-tab autopreview (ADR-0032).
- Bounded inert Markdown presentation and compact paragraph spacing; original MIME stays unchanged (ADR-0034).
- Signing-only sending and full release acceptance remain outstanding; these changes do not declare 1.0 complete.

- Mobile: tap the current view title to switch Spaces, user-defined labels or activity filters; native dialog and authenticated full-page fallback under `/views`.

Settings share one content width, first-position section navigation, and native current-section links across Accounts, Keys, Rules, Labels, Jev, Profile and Organizations. The compact navigation scrolls horizontally on narrow screens rather than changing page height; full-page fallbacks keep the same structure.


Feedback follow-up: thread footer opens a scoped reply composer for the selected original; panel navigation is captured before HTMX and close/sent refresh selects only the root fragment. Panels display a bounded loading state with stable dimensions. Reply subject prefixes Re:/Odp: are normalized for display only. Encryption unlock opens before asynchronous key listing and reports worker errors visibly; native Safari acceptance remains a separate check.

Feedback follow-up (MS-020): inline reply cancellation explicitly targets the root/thread fragment instead of inheriting the form's status target. Recorded plain sends now refresh the visible feed through the same event as encrypted sends; closing a thread after sending also refreshes the underlying timeline. Phone messages use the available width without an avatar gutter. Original-thread presentation keeps time, selected-source Reply and recorded SMTP state, without classification/status/Space toolbars; its sticky date divider uses the panel surface. Full local checks and live responsive/keyboard acceptance passed at 390×844, 1440×1000 and 1280×500 with synthetic mail; see VALIDATION.md. The next 1.0 implementation gate remains explicit signed-only sending.
