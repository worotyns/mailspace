# OpenPGP-first, nie własna kryptografia

OpenPGP jest częścią core UX od pierwszego projektu, implementacja jest bramką MVP. Nie utożsamiamy opisu granicy browser crypto z gotową ochroną.

## Format i interoperacyjność

OpenPGP/MIME wg [RFC 3156](https://www.rfc-editor.org/rfc/rfc3156.html), format OpenPGP wg [RFC 9580](https://www.rfc-editor.org/rfc/rfc9580.html). Biblioteka musi obsłużyć potrzebne istniejące klucze i uzgodniony profil nowego formatu; nie zakładamy, że każdy klient obsłuży wszystkie nowe algorytmy. Zachowanie nieobsługiwanych pakietów: jawny błąd i eksport oryginału, bez cichego downgrade.

Zachowujemy dokładne MIME i canonicalization CRLF do podpisów. Docelowo multipart/encrypted i multipart/signed; inline PGP co najwyżej jawny fallback odczytu. Testy importu/eksportu i round-trip z GnuPG oraz Thunderbirdem: podpisane, encrypted, signed+encrypted, multipart attachments, Unicode, malformed MIME, uszkodzony podpis, expired/revoked key, unknown signer i mixed recipients. Wersje narzędzi i algorytmy zapisać przy fixtures. Poprawny podpis nie oznacza zaufanej tożsamości.

## Klucze i UX

- Browser vault w IndexedDB zawiera tylko zaszyfrowane klucze; passphrase i unlocked keys wyłącznie w pamięci, explicit lock, idle timeout i lock przy wylogowaniu. Ochrona przed utratą: export/backup i recovery z jasną informacją, że operator nie odzyska hasła.
- Import istniejącego klucza i generowanie nowego po wyborze biblioteki; fingerprint, termin ważności, revocation, rotacja, subkeys, historia zaufania. Brak auto-upload private keys.
- WKD/Autocrypt to opcjonalne odkrywanie publicznych kluczy, nie automatyczna weryfikacja. Keyserver off domyślnie, widoczna sieć i ujawnienie wyszukiwanego adresu.
- Przy skonfigurowanym kluczu podpis domyślnie włączony. Szyfrowanie proponowane dla wszystkich zweryfikowanych odbiorców, włącznie z własną kopią do Sent; brak choć jednego klucza zatrzymuje encrypted send. Zwykła poczta pozostaje możliwa po jawnej decyzji.
- Drafts dla zaszyfrowanej treści lokalnie lub jako ciphertext; nigdy automatyczny plaintext autosave do IMAP/servera. Replies/quotes/forward nie mogą ominąć tej reguły.
- Nagłówki/envelope/rozmiary i timing pozostają widoczne; temat może pozostawać plaintext. Protected headers to przyszła decyzja po testach kompatybilności.

## Przyszły desktop agent / YubiKey

Browser nie zakłada bezpośredniego dostępu do gpg-agent czy OpenPGP card. Osobny ADR dla podpisanego desktop agent/bridge: origin binding, pairing, uprawnienie per operacja, potwierdzenie użytkownika, replay protection, brak ogólnego remote GPG RPC. Prywatny klucz sprzętowy nigdy nie jest eksportowany. WebAuthn nie zastępuje automatycznie interfejsu OpenPGP card. Funkcja nie blokuje MVP.

## Stan implementacji

MS-005: lokalny sejf, generowanie/import/export, lock i explicit fingerprint trust działają na `/keys`; wybór biblioteki i ograniczenia opisuje ADR-0012. MS-006 ma zintegrowaną ścieżkę alpha: podpisane szyfrowanie tekstu, opaque MIME/outbox i lokalny odczyt podpisanego multipart/encrypted. Pełna macierz kompatybilności, detached signatures i Thunderbird pozostają otwarte; globalne openpgp=false nie obiecuje jeszcze secure MVP. ADR-0013 opisuje dokładny zakres.

Załączniki i szkice: secure composer szyfruje do 10 plików / 1 MiB łącznie; jawny Save zapisuje lokalny ciphertext szkicu, także z plikami, odbiorcami i nonce. Load wymaga lokalnego odblokowania. Brak autosave/IMAP Drafts, cross-device sync i odzyskania z backupu serwera. Szczegóły ADR-0014.
