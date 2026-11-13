# Local rules

Open **Local rules** from Settings or Commands. Choose an exact email sender, exact domain (ASCII or punycode, without implicit subdomains), or subject phrase, and a destination Space. Save creates a disabled rule. **Preview cached matches** shows the eligible cached messages without assigning anything. Review, then explicitly enable it for cached and future incoming mail.

After assigning a message manually, **Make sender rule…** prefills a suggestion. It does not create or enable a rule silently. Editing disables the rule again. Disable or remove stops future work and keeps existing assignments. Manual organization wins: messages with manual membership changes are skipped by automatic rules. Several rules can place one message in several Spaces.

Rules run locally after successful mailbox sync; no model or API key is needed. Basic header rules do not inspect bodies. Own, encrypted, unavailable and manually organized messages are excluded. Matching does not move server folders, mark spam, send email or share anything. A Space named Spam is only a local organizational view.

Limits: 32 rules/user, 50 new decisions per pass, 5,000 stored receipts/user, 50,000 memberships/user. Previews show at most 50 eligible matches. Large caches progress over later successful syncs. This is bounded recent cached mail, not a full provider-archive search. Jev remains separately optional under its existing consent settings.

Back up the SQLite database and credential keyring before upgrading to schema 9. See [server setup](SERVER_SETUP.md) and [ADR-0027](adr/0027.md).

## Combined filters

The timeline search input also accepts conditions such as:

```text
(subject:invoice OR subject:receipt) AND domain:example.test
(from:notifications@example.test OR body:terapeuto) AND to:me@example.test
```

Use AND to require both conditions, OR for either, and parentheses to group them. AND has precedence over OR; adjacent conditions are AND. `from:` / `to:` match exact addresses, `domain:` matches an exact domain, and `subject:` / `body:` contain text without case sensitivity. Quote phrases, e.g. `body:"monthly payment"`. Plain search remains a metadata phrase search. Body filters scan up to 64 KiB of ordinary cached text, never encrypted/decrypted material, and create no index.

Click the rule icon next to search to draft the same conditions and person scope. Review the destination Space, save disabled, preview, then explicitly enable. Date and label filters are not part of this draft. In Settings → Rules, **Combined conditions** overrides the simple Match/Value fields when nonempty. Editing disables the rule again. Limits: 2,048 UTF-8 bytes, 16 conditions, four nested groups. See [ADR-0031](adr/0031.md). Upgrade schema is now 12; back up before upgrading and restore the matching database/image to roll back.
