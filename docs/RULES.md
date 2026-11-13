# Local rules

Open **Local rules** in the sidebar. Choose an exact email sender, exact domain (ASCII or punycode, without implicit subdomains), or subject phrase, and a destination Space. Save creates a disabled rule. **Preview cached matches** shows the eligible cached messages without assigning anything. Review, then explicitly enable it for cached and future incoming mail.

After assigning a message manually, **Make sender rule…** prefills a suggestion. It does not create or enable a rule silently. Editing disables the rule again. Disable or remove stops future work and keeps existing assignments. Manual organization wins: messages with manual membership changes are skipped by automatic rules. Several rules can place one message in several Spaces.

Rules run locally after successful mailbox sync; no model, API key or message body is needed. Own, encrypted, unavailable and manually organized messages are excluded. Matching does not move server folders, mark spam, send email or share anything. A Space named Spam is only a local organizational view.

Limits: 32 rules/user, 50 new decisions per pass, 5,000 stored receipts/user, 50,000 memberships/user. Previews show at most 50 eligible matches. Large caches progress over later successful syncs. This is bounded recent cached mail, not a full provider-archive search. Jev remains separately optional under its existing consent settings.

Back up the SQLite database and credential keyring before upgrading to schema 9. See [server setup](SERVER_SETUP.md) and [ADR-0027](adr/0027.md).
