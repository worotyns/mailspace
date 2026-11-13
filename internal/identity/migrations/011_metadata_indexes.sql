-- SPDX-License-Identifier: AGPL-3.0-only
-- Metadata/order/FK indexes only. No message body or decrypted material indexed.
CREATE INDEX mail_messages_time ON mail_messages(account_id,mailbox,received_at,uid);
DROP INDEX decision_owner_runs;
CREATE INDEX decision_owner_runs ON decision_runs(owner_id,created_at DESC,id DESC);
DROP INDEX outbox_account;
CREATE INDEX outbox_account ON outbox(account_id,created_at DESC,id DESC);
CREATE INDEX collections_owner_kind ON message_collections(owner_id,kind,created_at,id);
CREATE INDEX decision_runs_account ON decision_runs(account_id);
CREATE INDEX rule_runs_account ON message_rule_runs(account_id);
CREATE INDEX rules_space ON message_rules(space_id);
CREATE INDEX decision_active_work ON decision_settings(updated_at,owner_id) WHERE mode IN ('review','auto');
