-- SPDX-License-Identifier: AGPL-3.0-only
CREATE TABLE message_rules (
 id TEXT PRIMARY KEY,
 owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('sender','domain','subject')),
 value TEXT NOT NULL,
 space_id TEXT NOT NULL REFERENCES message_collections(id) ON DELETE CASCADE,
 enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 1,
 created_at INTEGER NOT NULL
);
CREATE INDEX message_rules_owner ON message_rules(owner_id,created_at,id);
CREATE TABLE message_rule_runs (
 id TEXT PRIMARY KEY,
 rule_id TEXT NOT NULL REFERENCES message_rules(id) ON DELETE CASCADE,
 rule_revision INTEGER NOT NULL,
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 account_revision INTEGER NOT NULL,
 message_key TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 UNIQUE(rule_id,rule_revision,account_id,account_revision,message_key)
);
