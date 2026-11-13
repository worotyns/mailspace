# Reading mail in personal mode

Connect an IMAP/SMTP account as described in MAIL_ACCOUNTS.md. After saving, Mailspace opens the real mailbox view and schedules a first download. Select **Refresh mail** for an immediate read. Subsequent polling runs in the background; reload or refresh the view to see new cached mail. The sidebar switches actual folders and connected accounts; Account & session settings remains available. On mobile, folder shortcuts and Settings remain accessible.

Incoming synchronization is read-only. Outgoing composition is separate (SENDING.md). Reading uses EXAMINE/BODY.PEEK and does not set Seen or change server flags. Synchronizing reconciles local flags and removed messages. Cached messages remain readable when a server is unavailable. A failure banner does not erase previously cached mail.

## Current explicit limits

- Latest 500 messages per folder, at most 32 selectable folders. Older history remains on the provider.
- Each cycle downloads 20 new bodies / at most 16 MiB. Pending rows resume on the next poll or Refresh.
- 4 MiB per original message and 64 MiB MIME cache per account. Large messages show a placeholder. Cache capacity is a limit, not a claim that every body was downloaded.
- Polling advances through eight accounts per minute. No IDLE push yet. Restart resumes durable cache checkpoints.
- Text/plain previews only. No remote image loads, rendered HTML, attachment download or OpenPGP decryption yet. Ordinary outgoing mail is available separately (SENDING.md).
- Filter searches cached subject/sender metadata only, without an index. Ciphertext is not decrypted or searched.
- Threading is limited to one account/folder. Ambiguous/missing references remain separate; duplicate Message-ID never identifies a stored message.

## Data and recovery

The SQLite cache contains original MIME and plaintext previews of ordinary, unencrypted provider mail. It is sensitive; keep the data directory and offline backups private. Provider credential encryption is separate and does not encrypt this cache. OpenPGP ciphertext stays ciphertext, with no plaintext preview. Appending or deleting cached rows never changes provider mail.

Changing provider account settings clears the old cache and increments its generation. New sync downloads from the new verified configuration. A UIDVALIDITY reset discards the old folder generation before reusing any UID. Restore/rollback follows IDENTITY.md and ADR 0009. Tests use only .test mail and the isolated development fixture.
