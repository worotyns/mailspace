-- SPDX-License-Identifier: AGPL-3.0-only
CREATE TABLE mail_import_settings (
 account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 mode TEXT NOT NULL CHECK(mode IN ('fresh','recent')),
 revision INTEGER NOT NULL DEFAULT 1,
 updated_at INTEGER NOT NULL
);
CREATE TABLE mail_import_baselines (
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 mailbox TEXT NOT NULL,
 account_revision INTEGER NOT NULL,
 uid_validity INTEGER NOT NULL,
 baseline_uid INTEGER NOT NULL,
 PRIMARY KEY(account_id,mailbox)
);
