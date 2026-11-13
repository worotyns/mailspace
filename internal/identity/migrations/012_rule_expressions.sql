-- SPDX-License-Identifier: AGPL-3.0-only
-- Local query text only; no message body index or model job.
ALTER TABLE message_rules ADD COLUMN expression TEXT NOT NULL DEFAULT '' CHECK(length(expression)<=2048);
