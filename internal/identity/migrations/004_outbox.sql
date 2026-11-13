-- SPDX-License-Identifier: AGPL-3.0-only
CREATE TABLE outbox (
 id TEXT PRIMARY KEY,
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 nonce TEXT NOT NULL,
 input_hash BLOB NOT NULL CHECK(length(input_hash)=32),
 revision INTEGER NOT NULL,
 recipients TEXT NOT NULL,
 raw BLOB NOT NULL CHECK(length(raw)<=4194304),
 subject TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','sending','accepted','rejected','unknown')),
 copy_state TEXT NOT NULL CHECK(copy_state IN ('not-attempted','copying','saved','failed','unknown')),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 UNIQUE(account_id,nonce)
);
CREATE INDEX outbox_account ON outbox(account_id,created_at);
