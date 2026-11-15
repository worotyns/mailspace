-- SPDX-License-Identifier: AGPL-3.0-only
-- Local full-text index over ordinary cached preview text (ADR-0046). It never
-- stores ciphertext, decrypted material, keys or provider credentials. Triggers
-- keep it in step with mail_messages; encrypted previews have empty text and
-- therefore contribute nothing.
CREATE VIRTUAL TABLE message_search USING fts5(body, tokenize = 'unicode61 remove_diacritics 2');
CREATE TRIGGER message_search_insert AFTER INSERT ON mail_messages BEGIN
  INSERT INTO message_search(rowid, body) VALUES (new.rowid, coalesce(json_extract(new.preview, '$.Text'), ''));
END;
CREATE TRIGGER message_search_update AFTER UPDATE OF preview ON mail_messages
WHEN coalesce(json_extract(old.preview, '$.Text'), '') IS NOT coalesce(json_extract(new.preview, '$.Text'), '')
BEGIN
  DELETE FROM message_search WHERE rowid = old.rowid;
  INSERT INTO message_search(rowid, body) VALUES (new.rowid, coalesce(json_extract(new.preview, '$.Text'), ''));
END;
CREATE TRIGGER message_search_delete AFTER DELETE ON mail_messages BEGIN
  DELETE FROM message_search WHERE rowid = old.rowid;
END;
INSERT INTO message_search(rowid, body)
SELECT rowid, coalesce(json_extract(preview, '$.Text'), '')
FROM mail_messages
WHERE coalesce(json_extract(preview, '$.Text'), '') <> '';
