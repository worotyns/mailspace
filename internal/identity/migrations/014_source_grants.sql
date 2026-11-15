-- SPDX-License-Identifier: AGPL-3.0-only
-- Bounded team-source grants (MS-009/MS-010 foundation). A grant authorizes one
-- organization member to read one account's cached mail. It never grants send
-- or OpenPGP keys. The read paths enforce it in a later slice; until then the
-- table is inert foundation and no member gains private-mail access.
CREATE TABLE source_grants (
 id TEXT PRIMARY KEY,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 mailbox TEXT NOT NULL DEFAULT '',
 grantor_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 grantee_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 can_download INTEGER NOT NULL DEFAULT 0 CHECK(can_download IN (0,1)),
 created_at INTEGER NOT NULL,
 UNIQUE(account_id,mailbox,grantee_id)
);
CREATE INDEX source_grants_grantee ON source_grants(grantee_id,account_id);
CREATE INDEX source_grants_grantor ON source_grants(grantor_id,workspace_id);
-- Team Spaces are linked to an organization. Existing rows stay private (NULL).
ALTER TABLE message_collections ADD COLUMN workspace_id TEXT REFERENCES workspaces(id) ON DELETE CASCADE;
