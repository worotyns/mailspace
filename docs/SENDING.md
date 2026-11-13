# Sending and replying — M1 alpha

Use New email or Reply in the root Space dialog. View email thread opens original cached context; Reply there targets that specific original message. Check recipients and use explicit Send unencrypted, or Encrypt with a local unlock modal. Sender/account and original References are preserved server-side. Replies to your own messages target an original To recipient. No quote is automatically inserted; this is not Reply All.

Inline compose supports plain UTF-8 text and local signed encryption. The advanced encrypted composer provides To/Cc/Bcc, encrypted attachments and encrypted browser-local drafts. Missing/untrusted keys and encrypted-parent downgrade fail closed. Neither SMTP-over-TLS nor ordinary mail implies an OpenPGP signature. Local draft/key exports are separate from server backups. See CLIENT_CRYPTO.md and RELEASE_READINESS.md for exact limits.

## Outbox states

| State | Meaning |
|---|---|
| Prepared | Stored, no send attempt claimed yet |
| Sending | One SMTP attempt is in progress |
| Accepted | SMTP server accepted the message; recipient delivery is not confirmed |
| Rejected | Not accepted; no automatic retry |
| Unknown | Connection/crash prevented a trustworthy final result; check the provider before composing another copy |

Submitting the same form again resolves to its original record. It does not resend an accepted, rejected or unknown message. Refreshing Outbox is a read. Unknown status is intentionally conservative; the recipient might already have the message. To send again, create a new message deliberately after checking the provider. There is no automatic resend button or background delivery retry worker.

## Sent copy

Account settings include **Sent folder (exact IMAP name)**. Default: Sent. Use the actual existing folder, including any namespace prefix or localized name. The app does not create it automatically. A successful SMTP attempt gets one APPEND attempt into that folder. Copy saved means the IMAP acknowledgement succeeded; Copy unconfirmed means it may or may not exist. SMTP is never repeated to repair a missing copy. Some providers save sent mail automatically too, which can produce duplicate Sent copies; automatic provider-specific deduplication is not implemented.

Refresh the mailbox after sending to read its Sent folder. Outbox retains at most 500 records / 64 MiB per account, with no automatic deletion yet. It contains sensitive ordinary MIME and envelope recipients, including Bcc, scoped to the owner. Bcc never enters recipient-visible MIME. Protect offline backups as described in IDENTITY.md. Run one web process per SQLite installation. ADR 0010 records the trust and recovery decisions.
