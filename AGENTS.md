# Mailspace — zasady pracy agentów i kontrybutorów

Te instrukcje obowiązują w całym repo. Zmiana produktu wymaga spełnienia poniższych bramek, nie tylko działającego happy path.

## Zakres i sposób pracy

1. Przeczytaj README, docs/ROADMAP.md, właściwy ADR i issue przed zmianą. Wybierz jeden mały wycinek; nie implementuj całej roadmapy w jednym PR.
2. Pisz kod i identyfikatory po angielsku. Dokumentacja produktowa może być po polsku. Każde TODO musi wskazywać MS-xxx lub opis blokady.
3. Nie nadpisuj cudzych zmian. Sprawdź `git status` i diff; dodawaj do commita wyłącznie pliki własnego wycinka. Nie force-pushuj, nie publikuj release ani nie wysyłaj wiadomości bez upoważnienia.
4. Po skończonej jednostce pracy: testy, przegląd diff, aktualizacja statusu dokumentacji, konkretny commit. Zgłaszaj wykonane i niewykonane checki. Nie deklaruj obsługi funkcji będącej atrapą.
5. Każda decyzja zmieniająca granicę zaufania, format danych, moduły lub storage wymaga ADR, migracji/rollbacku (jeśli dotyczy) i testu regresji.
6. Własny kod AGPL-3.0-only; nagłówek SPDX w nowych plikach kodu. Nie dodawaj zależności z niezgodną lub niejasną licencją. Aktualizuj lockfile, sprawdź lifecycle scripts i transitive dependencies. Brak CLA do zamkniętego relicencjonowania.

## Architektura

- Modularny monolit Go + html/template + HTMX + Tailwind CSS. Domain nie importuje httpapi ani adapterów. Interfejs twórz tam, gdzie istnieje realna granica, nie abstrakcję do każdej funkcji.
- SQLite jest domyślnym planem storage; PostgreSQL opcjonalny. Nie dodawaj Redis, brokera, Elasticsearch ani modelu jako zależności core.
- Rozszerzenia są wbudowane, poddawane review. Nie dodawaj publicznego plugin SDK, dynamicznego ładowania, marketplace, arbitralnych skryptów lub stabilnej obietnicy kompatybilności zewnętrznego API.
- Contribution: fork → PR → review → merge. Moduł ma jawne wejścia/wyjścia, capability, stan disabled i kontrolowany lifecycle.
- Wyłączony moduł nie uruchamia indeksowania, jobów, klienta providera, inicjalizacji modelu ani połączeń zewnętrznych.

## Prywatność i kryptografia — twarde ograniczenia

- Nigdy nie dodawaj endpointu przyjmującego prywatne klucze OpenPGP ani ich passphrase. Nie loguj body, nagłówków poczty, tokenów ani sekretów.
- Backend przenosi MIME/ciphertext. Odszyfrowany materiał ma provenance `client-decrypted`; nie może trafić do backendowej bazy, indeksu, logów, trace, reguł, AI ani webhooków przez ogólny eksport.
- Ewentualne świadome przekazanie odszyfrowanej treści wymaga osobnego ADR i akcji w kliencie: konkretny fragment, cel/provider, odbiorca, czas, retencja i możliwość anulowania. Nie wystarczy globalny checkbox.
- Klucze lokalnie: zaszyfrowany vault, unlock tylko w pamięci, auto-lock i eksport/recovery. Nie przechowuj odblokowanych kluczy w localStorage ani state persisted do serwera.
- Nie implementuj własnych prymitywów crypto. Bibliotekę OpenPGP wybierz po sprawdzeniu utrzymania, licencji i testów GnuPG/Thunderbird. OpenPGP/MIME to bramka MVP, nie przyszły plugin.
- Odkrycie klucza (WKD/Autocrypt) nie oznacza zaufania. Brak lub zmiana klucza blokuje szyfrowaną wysyłkę; plaintext wymaga jawnej decyzji.
- HTML z maila jest wrogim dokumentem: sanitizacja, izolacja, blokada zewnętrznych zasobów, brak `dangerouslySetInnerHTML` bez audytowanej granicy. Limity rozmiaru MIME, zagnieżdżeń i dekompresji.
- Nie twórz fetchera do dowolnych URL/hostów. IMAP/SMTP onboarding wymaga polityki SSRF, TLS verify, limitów i timeoutów; wyjątki self-hosting muszą być jawne i administracyjne.
- Wszystkie zasoby są scoped do user/account/workspace. Sprawdzaj autoryzację przy odczycie, zmianie, pobraniu załącznika, jobie, SSE/WS i udostępnieniu. Testy dwóch użytkowników są obowiązkowe.

## UI — Go + HTMX + Tailwind

- Go html/template renderuje dokument i fragmenty HTML. HTMX obsługuje nawigację i aktualizacje, z pełnym href/action fallback bez JS. Nie dodawaj React/Vue/Mantine ani kolejnego SPA toolchainu.
- Tailwind jest podstawą. Używaj jego utilities i wspólnych tokenów; własne reguły CSS tylko dla realnych potrzeb poczty/układu. Nie buduj własnego rozbudowanego systemu komponentów. Tabler jest jednym zestawem ikon.
- Jeden dialog Spaces pod `/`: wizualne grupy kolejnych wiadomości autora, osobne własne odpowiedzi, oryginalny wątek jako podgląd z odpowiedzią do wybranej wiadomości. Nie przywracaj Classic/Conversation/Calendar ani osobnego UI demo. Synthetic dev sandbox używa tego samego produktu.
- Assety są lokalne, przypięte lockfile, osadzone w binarce. Node służy tylko do builda. Brak Tailwind CDN, zdalnych fontów i dynamicznie wykonywanych fragmentów JS.
- Poczta jest niezaufanym tekstem, Go autoescape jest obowiązkowe. Nie rzutuj treści wiadomości na template.HTML. Wyjątek: sprawdzony renderer HTML według osobnego issue/ADR.
- HTMX: allowEval=false, allowScriptTags=false, historyCacheSize=0, selfRequestsOnly=true; hx-history=false. Dla prywatnych danych zakaz browser history snapshot/cache. Bez inline hx-on i eval.
- Decrypt/vault to browser-only moduły JS w web/crypto; ich granice opisują ADR-0012/0013. Nigdy nie przesyłaj private keys/passphrase/decrypted plaintext przez hx-post, hx-vals, hidden fields, logi czy server template. Obszar crypto wyłączony z HTMX, testowany pod kątem egress. Szczegóły docs/CLIENT_CRYPTO.md.
- RWD: sprawdzaj co najmniej telefon 390px, desktop i niskie okno. Aplikacja ma jeden viewport; sidebar/feed/thread przewijają się niezależnie, bez wycieku do document scroll. Native settings/composer mają zwykły scroll dokumentu.
- System/Light/Dark przez semantyczne tokeny Tailwind; nie dodawaj kolejnych sztywnych białych powierzchni. W localStorage tylko preferencja wyglądu, nigdy poczta/klucze/odszyfrowany stan.
- Spaces to widoki z regułami, nie uprawnienia. Match reguły nie udostępnia prywatnej wiadomości. Team Space wymaga membership oraz ACL źródła/wątku; send-as osobno. Szczegóły docs/SPACES.md.
- Skok do daty i oryginalny wątek operują na cached mail, bez obietnicy pełnej historii. Timestamp i strefa IANA muszą być spójne. Nie łącz wątków po samym subject ani między kontami/rewizjami.
- Zawsze error/empty/loading state, klawiatura/focus/labels i mobile. Nie udawaj wysyłki, pobrania załącznika ani poprawnego podpisu, gdy to fixture.

## Testy i odbiór

Przed commitem uruchom `gofmt`, `go vet ./...`, `go test -race ./...`, `make check` (buduje assets, sprawdza formatting, vet i race tests). Dla kontenera: `docker compose config --quiet`, build i smoke test health/API/SPA. Jeśli narzędzie nie jest dostępne, zapisz to w raporcie, nie oznaczaj jako passed.

- Testuj zachowania i zagrożenia, nie kopie implementacji. Zmiany uprawnień: test odmowy między kontami. Zmiany eksportu: macierz provenance/destination/consent i nieznane wartości fail-closed.
- Mail: reconnect, UIDVALIDITY reset, duplikaty Message-ID, missing References, timeout SMTP i niejednoznaczna wysyłka. Nigdy automatycznie ponownie nie wysyłaj maila po niejednoznacznym wyniku.
- Crypto: MIME canonicalization, niepoprawny podpis, untrusted key, brak klucza, mixed recipients, round-trip GnuPG/Thunderbird, brak wycieku w żądaniach i logach.
- UI: build Tailwind/HTMX i render test; dla zmian interakcji sprawdź klawiaturę i dwa rozmiary ekranu. Nie twórz snapshotów tylko po to, by zwiększyć liczbę testów.
- Nie uruchamiaj testów z prawdziwą skrzynką/sekretami. Używaj syntetycznych .test fixtures i izolowanego serwera testowego.

## Definition of done

Mały zakres ukończony; wymagane testy wykonane; brak sekretów; dokumentacja i capabilities zgodne z kodem; review diff; commit. Publikacja i wdrożenie są osobnymi krokami. Nie pomijaj ograniczeń w końcowym raporcie.

Zmiany workflows sprawdzaj actionlint. Build assetów dołącza upstream THIRD_PARTY_NOTICES.txt; brak licencji zależności blokuje build, nie pomijaj tego kroku.

HTML mail stays text-first: prefer native text/plain, convert HTML-only to inert text. HTML preview is an explicit native dialog. Its only approved HTML sink is sanitized `iframe.srcdoc` in `web/crypto/mail-html.mjs`, inside hx-disable, with an empty sandbox (no scripts/same-origin), no resource/navigation attributes, restrictive CSP and a hash for fixed application styling. This narrow client-only rendering exception does not permit decrypted material in generic data attributes, hidden form fields, SSR, HTMX payloads or any backend export. Closing/locking removes the frame and source document. See ADR-0015.

Product direction ADR-0016 is authoritative: Spaces aggregate individual messages many-to-many, never whole email threads by default. Spaces/People/labels use one timeline; provider folders and the legacy presentation tabs are not primary navigation. Reply to a selected message inline, visibly preserving its original email recipient/account/References. Encryption is an option with a small modal, with no silent encrypted-parent downgrade. Space definitions are classification briefs; manual triage works without a model. Any optional JEV/OpenRouter adapter must validate scope, provenance, revisions and human overrides; it cannot send mail or grant sharing.
