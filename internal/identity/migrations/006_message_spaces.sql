-- SPDX-License-Identifier: AGPL-3.0-only
CREATE TABLE message_collections (
 id TEXT PRIMARY KEY,
 owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('space','label')),
 name TEXT NOT NULL,
 description TEXT NOT NULL DEFAULT '',
 color TEXT NOT NULL CHECK(color IN ('lilac','blue','green','peach','rose','amber')),
 revision INTEGER NOT NULL DEFAULT 1,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 UNIQUE(owner_id,kind,name COLLATE NOCASE)
);
CREATE TABLE message_collection_memberships (
 collection_id TEXT NOT NULL REFERENCES message_collections(id) ON DELETE CASCADE,
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 account_revision INTEGER NOT NULL,
 message_key TEXT NOT NULL,
 source TEXT NOT NULL CHECK(source IN ('manual','compose','rule','model')),
 actor_id TEXT NOT NULL REFERENCES users(id),
 created_at INTEGER NOT NULL,
 PRIMARY KEY(collection_id,message_key)
);
CREATE INDEX message_collection_source ON message_collection_memberships(account_id,account_revision,message_key);
CREATE TABLE message_manual_revisions (
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 account_revision INTEGER NOT NULL,
 message_key TEXT NOT NULL,
 revision INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(account_id,account_revision,message_key)
);
