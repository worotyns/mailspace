-- SPDX-License-Identifier: AGPL-3.0-only
-- Keep the legacy color column and its check intact; no parent-table rebuild.
ALTER TABLE message_collections ADD COLUMN palette_color TEXT CHECK(palette_color IN ('lilac','blue','green','peach','rose','amber','red','pink','fuchsia','purple','indigo','cyan','teal','lime','yellow','slate'));
CREATE TABLE message_archives (
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 account_revision INTEGER NOT NULL,
 message_key TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 PRIMARY KEY(account_id,account_revision,message_key)
);
