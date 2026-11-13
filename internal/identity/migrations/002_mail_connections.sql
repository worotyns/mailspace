-- SPDX-License-Identifier: AGPL-3.0-only
CREATE TABLE mail_connections (
 account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 config TEXT NOT NULL CHECK(length(config) <= 4096),
 imap_key_id TEXT NOT NULL CHECK(length(imap_key_id) = 32),
 imap_secret BLOB NOT NULL CHECK(length(imap_secret) <= 32768),
 smtp_key_id TEXT NOT NULL CHECK(length(smtp_key_id) = 32),
 smtp_secret BLOB NOT NULL CHECK(length(smtp_secret) <= 32768),
 checked_at INTEGER NOT NULL
);
