# Contributing

Fork → branch → mały PR → review → merge. Brak publicznych pluginów; nowy moduł proponuj jako zmianę w repo z capability, polityką danych i testami. Przed większą zmianą opisz problem i ADR, żeby uniknąć implementacji sprzecznej z granicami bezpieczeństwa.

Przeczytaj AGENTS.md. `make check`; Docker checks wg docs/OPERATIONS.md. Do PR dołącz zachowanie przed/po, walidację, wpływ na prywatność/ACL i screenshot dla UI. Zachowaj AGPL-3.0-only własnego kodu, prawa autorów i licencje zależności; bez CLA do komercyjnego relicencjonowania. Commity podpisuj DCO (`git commit -s`) własną rzeczywistą tożsamością; nie podszywaj się pod innych.

Public repository: https://github.com/worotyns/mailspace. Open an issue with reproduction steps before a large change; use synthetic .test data and redact logs. The project is early alpha and does not promise review or release deadlines.
