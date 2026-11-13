# Threat model — wersja 0.1

Current implementation and release limits are summarized in [RELEASE_READINESS.md](RELEASE_READINESS.md) and ADR-0019. Per-milestone status notes below are historical scope records; authenticated personal mode, browser crypto and the root Space dialog now replace the old public demo. This document is a threat model, not an independent security-audit attestation.


## Aktywa i przeciwnicy

Aktywa: credentiale IMAP/SMTP, sesje, prywatne klucze, passphrase, plaintext, metadane, załączniki, ACL, kopie zapasowe. Przeciwnicy: złośliwy nadawca, użytkownik innego konta/zespołu, dostawca sieci/modelu, zainfekowany pakiet, kompromitowany serwer webmaila, złośliwe rozszerzenie przeglądarki.

Granice: internet→TLS edge; browser→Go; Go→IMAP/SMTP; Go→storage/backup; wiadomość→renderer; browser→vault; opcjonalny provider→jawna akcja użytkownika. Administrator instancji jest zaufany do dostarczania JavaScript, nie powinien otrzymywać kluczy przez projektowany protokół.

| Zagrożenie | Kontrola / test odbiorczy | Ryzyko pozostające |
|---|---|---|
| XSS / EFAIL / zdalne zasoby w mailu | plaintext default; audytowana sanitizacja + sandbox HTML, brak skryptów/form i automatycznych obrazów; corpus malicious MIME | błąd renderera lub rozszerzenie może wykraść plaintext |
| Serwer podmienia aplikację JS | CSP, zamrożone zależności, source archive, provenance release; przyszły podpisany desktop agent | browser crypto nie chroni przed złośliwym serwerem dostarczającym JS |
| Przejęcie klucza lokalnego | encrypted vault, auto-lock, unlock tylko RAM, recovery/export; brak server API | odblokowany klient, malware, keylogger |
| Cross-account / IDOR / shared link | autoryzacja każdego zasobu/job/SSE, deny default, testy 2 kont i revoke | uprawniony odbiorca może zachować kopię |
| Kradzież sesji / CSRF | secure HttpOnly SameSite cookies, rotation, logout/revoke, origin/CSRF kontrola mutacji, rate limits | przejęty klient |
| SSRF przez IMAP/SMTP/WKD | allowlist lub jawna admin polityka LAN, walidacja DNS/rebinding i portów, TLS verify, timeouty | self-hosted serwery w LAN wymagają kontrolowanego wyjątku |
| MIME bombs / malicious attachments | limity fetch/parse/decompress, bez wykonywania, download jako attachment, izolowane preview | podatność parsera/preview |
| Cichy wyciek do search/AI/webhook | provenance + fail-closed policy, moduły off, test braku egress, brak body w trace | jawnie zaakceptowany zewnętrzny odbiorca ma własną retencję |
| Prompt injection | AI nie ma uprawnień do wysyłki, reguł ani sekretów; potwierdzenie akcji | mylące podsumowania, treść modelu niezaufana |
| Logs/backups | redakcja, minimum danych, ograniczona retencja, szyfrowany backup i test restore | admin backendu widzi zwykłą pocztę i credentiale w czasie pracy |
| SMTP retry / replay | outbox state + identyfikator, brak auto retry niejednoznacznego send, potwierdzenie użytkownika | exactly-once SMTP nie jest gwarantowane |
| Spoofing kluczy/wątków | fingerprint trust, zmiany kluczy widoczne, Message-ID nie jest tożsamością | zaufanie do człowieka i weryfikacja poza kanałem |

## Reguły przepływu

- `metadata` także jest wrażliwe. Wyjście do dodatkowego modułu wymaga enablement i zgody; unknown class/destination odrzucane.
- `ciphertext` nie jest materiałem do modeli ani indeksu treści.
- `client-decrypted` nigdy nie trafia przez backendowy eksport. Jedyny jawny wyjątek to ADR-0021: konkretny fragment, provider/model i zgoda na jeden request; backend relay w pamięci, bez zapisu treści.
- Lokalny model na serwerze nadal jest poza browser trust boundary. Słowo „local” nie jest zgodą na odszyfrowany upload.
- Trace/replay reguł zawiera decyzje i minimalne referencje, nie body i klucze. Zrzuty debugowania również podlegają polityce.

## Bramki przed prawdziwą pocztą

Uwierzytelnienie i izolacja kont; TLS credential storage/rotation; SSRF; limity MIME; testy braku plaintext w HTTP/logach/DB dla zaszyfrowanych maili; przegląd crypto i interop; backup/restore; bezpieczne HTML; licencje i skan zależności. M0 nie spełnia tych bramek i nie jest wydaniem produkcyjnym.

## Status MS-001

W personal mode wdrożono local users/Argon2id, SQLite/session digests, __Host- Secure cookie (jawny loopback dev wyjątek), Origin+CSRF, rate/concurrency limits i owner-scoped account metadata. Nie obejmuje to jeszcze IMAP/SMTP, parsera mail HTML ani crypto. Demo pozostaje publiczną syntetyczną demonstracją, oddzieloną od personal mode.

## Status MS-002

Mail endpoints są domyślnie zablokowane; admin zatwierdza exact host:port, osobno LAN. DNS jest walidowany i pinowany do połączenia, TLS/SNI zweryfikowane, brak plaintext fallback, debug transcripts wyłączone. Provider login data jest szyfrowane AES-GCM z AAD owner/account/protocol i kluczem spoza DB. Klucze GPG nadal wyłącznie w browser; odszyfrowany fragment może zostać jawnie przekazany według ADR-0021. Pełny backend/process compromise nie jest objęty ochroną server credential vault; DB-only disclosure jest.

Read-only cache: original ordinary MIME and plain-text previews are sensitive unencrypted SQLite data. OpenPGP parts remain opaque; any encrypted sibling suppresses the whole plaintext preview. No HTML/remote resources, decryption or content index. UIDVALIDITY and account revisions prevent stale identity reuse; partial network failures do not authorize cache-wide deletes. Local DB or host compromise can expose cached ordinary mail. Limits and remaining native-IDLE/full-history work: ADR 0009 / MS-018.

Outgoing ambiguity: a missing final SMTP response is unknown, not failed-and-retryable. Nonce uniqueness plus a pending→sending CAS prevent duplicate form submissions; startup never retries interrupted sends. SMTP acceptance is distinct from recipient delivery and from a Sent copy. Bcc is envelope-only, with owner-scoped server storage. Plaintext send requires explicit per-message consent; ordinary compose cannot downgrade an encrypted reply.

Workspace boundaries: organization members can read the member roster but gain no private account, credential, outbox or send-as permission. Revoked memberships fail subsequent reads/switches; active context falls back to Personal. Names are untrusted escaped text; all changes require Origin/CSRF. Theme localStorage contains only a color preference. Calendar receives authorized metadata, not decrypted content. The development sandbox is numeric-loopback-only and permits only its synthetic mail endpoints.

Browser OpenPGP/MIME alpha: secure controls have no form/names and only explicitly constructed ciphertext JSON can reach submission. All recipients plus own key require current explicit bindings. Raw downloads are owner/generation/UID-scoped. Decryption and attachment Blob URLs remain local; textContent, no HTML/remote resources, lock clears DOM/URLs. Structural MIME validation does not prove crypto validity; only the client verifies signatures/trust. Full compatibility and Thunderbird remain pending (ADR-0013), so this is not a production crypto audit.

Selected-excerpt classification explicitly adds the operator backend/proxy and provider to the trust boundary for that excerpt. Server/provider compromise and retention remain risks; closing a dialog cannot recall transmitted bytes. No body logging at proxies/APM is allowed. Model IDs/confidence are untrusted, bounded and cannot authorize sending/sharing. Continuous replicas contain sensitive plaintext mail/metadata and require private storage, access limits and verified restoration with the separate credential keyring.
