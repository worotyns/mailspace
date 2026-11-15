-- SPDX-License-Identifier: AGPL-3.0-only
ALTER TABLE users ADD COLUMN auth_version INTEGER NOT NULL DEFAULT 1 CHECK(auth_version > 0);
CREATE TABLE user_mfa (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 key_id TEXT NOT NULL,
 secret BLOB NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 setup_session BLOB CHECK(setup_session IS NULL OR length(setup_session)=32),
 setup_version INTEGER NOT NULL,
 setup_expires INTEGER NOT NULL,
 last_step INTEGER NOT NULL DEFAULT -1,
 failures INTEGER NOT NULL DEFAULT 0 CHECK(failures >= 0),
 failure_since INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE mfa_recovery (
 user_id TEXT NOT NULL REFERENCES user_mfa(user_id) ON DELETE CASCADE,
 code_hash BLOB NOT NULL CHECK(length(code_hash)=32),
 PRIMARY KEY(user_id,code_hash)
);
CREATE TABLE mfa_challenges (
 token_hash BLOB PRIMARY KEY CHECK(length(token_hash)=32),
 user_id TEXT NOT NULL REFERENCES user_mfa(user_id) ON DELETE CASCADE,
 csrf_token TEXT NOT NULL,
 auth_version INTEGER NOT NULL,
 expires_at INTEGER NOT NULL,
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0 AND attempts <= 5)
);
CREATE INDEX mfa_challenges_user ON mfa_challenges(user_id);
CREATE INDEX mfa_challenges_expiry ON mfa_challenges(expires_at);
