-- SPDX-License-Identifier: AGPL-3.0-only
ALTER TABLE mail_connections ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;
CREATE TABLE mailboxes (
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 uid_validity INTEGER NOT NULL,
 total INTEGER NOT NULL,
 synced_at INTEGER NOT NULL,
 PRIMARY KEY(account_id,name)
);
CREATE TABLE mail_messages (
 account_id TEXT NOT NULL,
 mailbox TEXT NOT NULL,
 uid INTEGER NOT NULL,
 flags TEXT NOT NULL,
 size INTEGER NOT NULL,
 received_at INTEGER NOT NULL,
 raw BLOB,
 preview TEXT NOT NULL,
 PRIMARY KEY(account_id,mailbox,uid),
 FOREIGN KEY(account_id,mailbox) REFERENCES mailboxes(account_id,name) ON DELETE CASCADE
);
