# Backlog

Status: specyfikacje lokalne, nie zgłoszenia utworzone na GitHub. MS-001–MS-004 zaimplementowane lokalnie jako mail alpha (MS-003: bounded recent-mail window); MS-005 local key vault ukończony; następne MS-006: browser OpenPGP/MIME. M0 scaffold przygotowany; MS-017 ma definicję pipeline, ale wymaga pierwszego uruchomienia na GitHub.

| ID | Etap | Priorytet | Issue | Zależności |
|---|---|---|---|---|
| MS-001 | M1 | P0 | [Identity, SQLite i sesje](issues/MS-001.md) | M0 |
| MS-002 | M1 | P0 | [Konta IMAP/SMTP i credential store](issues/MS-002.md) | 001 |
| MS-003 | M1 | P0 | [Read-only IMAP sync](issues/MS-003.md) | 002 |
| MS-004 | M1 | P0 | [SMTP outbox i bezpieczny MIME](issues/MS-004.md) | 003 |
| MS-005 | M2 | P0 | [Crypto spike i client key vault](issues/MS-005.md) | M0 |
| MS-006 | M2 | P0 | [OpenPGP/MIME i bezpieczny composer](issues/MS-006.md) | 004,005 |
| MS-007 | M2 | P1 | [Threading i dwa widoki produkcyjne](issues/MS-007.md) | 003 |
| MS-008 | M2 | P0 | [Renderer i attachments security](issues/MS-008.md) | 003,006 |
| MS-009 | M3 | P1 | [Team workspace i shared mailbox](issues/MS-009.md) | 001,006 |
| MS-010 | M3 | P1 | [Thread share, tagi i przypisania](issues/MS-010.md) | 009,007 |
| MS-011 | M4 | P2 | [Attachment Drive](issues/MS-011.md) | 008,010 |
| MS-012 | M4 | P2 | [Full-text bez modeli](issues/MS-012.md) | 003,006 |
| MS-013 | M5 | P2 | [Deterministyczny decision engine](issues/MS-013.md) | 003 |
| MS-014 | M6 | P2 | [AI compose/summarize i semantic search](issues/MS-014.md) | 006,012 |
| MS-015 | M7 | P2 | [PostgreSQL adapter](issues/MS-015.md) | 001 |
| MS-016 | M7 | P2 | [Pakiet YunoHost](issues/MS-016.md) | M2 |
| MS-017 | M0 | P0 | [Reproducible release pipeline i smoke](issues/MS-017.md) | M0 |

Szacunki dopiero po M1 spike. P0: bramka bezpieczeństwa/core; P1: UX/team; P2: moduły i dystrybucja. Kolejność etapów w ROADMAP.

| MS-018 | M1 follow-up | P1 | [Full-history sync and native IDLE](issues/MS-018.md) | 003 |

| MS-019 | M1 UX | P0 | [Responsive themes, mail calendar and navigation](issues/MS-019.md) | 001,003,004 |

MS-009 foundation brought forward for usable organization navigation; actual mail sharing remains planned.

| MS-020 | Core UX | P0 | [Message-level Spaces and unified timeline](issues/MS-020.md) | 003,004,005 |

ADR-0016 is the current product direction. MS-020 precedes optional provider expansion; its source/storage foundation and first unified UI/inline reply slice are implemented; the optional decision provider and team ACL integration remain open.

MS-020 personal UX is implemented: root dialog, original thread preview/reply, private collection management and retired legacy UI. Stateful deployment and operator recovery are covered by SERVER_SETUP.md and ADR-0019. Open 1.0 gates remain in RELEASE_READINESS.md; optional providers/team ACLs are not claimed complete.
