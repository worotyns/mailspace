-- SPDX-License-Identifier: AGPL-3.0-only
-- Per-user read state for mail shared through a grant (ADR-0047). The teammate
-- cannot write provider flags on another owner's account, so their read state is
-- local to their user and never changes the owner's view. Rows are keyed by the
-- opaque message key and are harmless if the underlying message disappears.
CREATE TABLE shared_read_state (
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 message_key TEXT NOT NULL,
 unread INTEGER NOT NULL DEFAULT 0 CHECK(unread IN (0,1)),
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(user_id,message_key)
);
