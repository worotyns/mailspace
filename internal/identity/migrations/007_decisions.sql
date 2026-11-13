-- SPDX-License-Identifier: AGPL-3.0-only
CREATE TABLE decision_settings (
 owner_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 mode TEXT NOT NULL CHECK(mode IN ('off','manual','review','auto')),
 provider TEXT NOT NULL,
 payload TEXT NOT NULL CHECK(payload IN ('metadata','text')),
 revision INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE TABLE decision_runs (
 id TEXT PRIMARY KEY,
 owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 cache_key TEXT NOT NULL,
 snapshot TEXT NOT NULL,
 decision TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('pending','error','review','dry-run','applied','rejected')),
 created_at INTEGER NOT NULL,
 UNIQUE(owner_id,cache_key)
);
CREATE INDEX decision_owner_runs ON decision_runs(owner_id,created_at DESC);
