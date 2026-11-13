-- SPDX-License-Identifier: AGPL-3.0-only
CREATE TABLE users (
 id TEXT PRIMARY KEY,
 email TEXT NOT NULL UNIQUE,
 password_hash TEXT NOT NULL,
 created_at INTEGER NOT NULL
);
CREATE TABLE accounts (
 id TEXT PRIMARY KEY,
 owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 created_at INTEGER NOT NULL
);
CREATE INDEX accounts_owner ON accounts(owner_id);
CREATE TABLE sessions (
 token_hash BLOB PRIMARY KEY CHECK(length(token_hash) = 32),
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 csrf_token TEXT NOT NULL,
 expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expiry ON sessions(expires_at);
