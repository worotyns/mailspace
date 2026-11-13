-- SPDX-License-Identifier: AGPL-3.0-only
ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC';
CREATE TABLE workspaces (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 owner_id TEXT NOT NULL REFERENCES users(id),
 created_at INTEGER NOT NULL
);
CREATE TABLE workspace_members (
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 PRIMARY KEY(workspace_id,user_id)
);
CREATE INDEX workspace_members_user ON workspace_members(user_id);
CREATE TABLE user_preferences (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 active_workspace TEXT REFERENCES workspaces(id) ON DELETE SET NULL
);
CREATE TABLE workspace_audit (
 id TEXT PRIMARY KEY,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 actor_id TEXT NOT NULL REFERENCES users(id),
 target_id TEXT REFERENCES users(id),
 action TEXT NOT NULL CHECK(action IN ('created','renamed','member-added','member-removed')),
 created_at INTEGER NOT NULL
);
