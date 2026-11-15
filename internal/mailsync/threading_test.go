// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"strings"
	"testing"
)

func TestParseRetainsDirectReplyParentAlongsideReferences(t *testing.T) {
	raw := []byte("From: sender@example.test\r\nSubject: Odp: Siema\r\nMessage-ID: <child@example.test>\r\nReferences: <older@example.test>\r\n\t<shared@example.test>\r\nIn-Reply-To: <shared@example.test> <direct@example.test>\r\n\r\nSynthetic body")
	preview := Parse(raw)
	if preview.ThreadingVersion != threadingVersion || strings.Join(preview.References, ",") != "older@example.test,shared@example.test,direct@example.test" {
		t.Fatal("direct parent lost", preview.References)
	}
	repaired, ok := repairedReferences(raw)
	if !ok || strings.Join(repaired, ",") != strings.Join(preview.References, ",") {
		t.Fatal("cached header repair differs from fresh parse")
	}
	opaque := Preview{ContentHash: "original-opaque-hash", MessageID: "same@example.test", Encrypted: true}
	repairedPreview, encoded, ok := repairedPreview(opaque, raw)
	if !ok || !repairedPreview.Encrypted || repairedPreview.Text != "" || repairedPreview.ContentHash != opaque.ContentHash || repairedPreview.MessageID != opaque.MessageID || strings.Contains(encoded, "Synthetic body") {
		t.Fatal("repair touched body/provenance/identity")
	}
	overlong := []byte("X-Long: " + strings.Repeat("x", maxThreadHeaders) + "\r\n\r\n")
	if _, ok = repairedReferences(overlong); ok {
		t.Fatal("header repair exceeded byte budget")
	}
}

func TestCachedThreadRepairIsHeaderOnlyOwnedAndIdempotent(t *testing.T) {
	service, _, db, owner, other, account, reader := setup(t)
	reader.raw[2] = synthetic(2, "References: <uncached@example.test>\r\nIn-Reply-To: <m1@example.test>\r\n")
	ctx := context.Background()
	if err := service.Sync(ctx, owner, account); err != nil {
		t.Fatal(err)
	}
	var stored string
	var originalRaw []byte
	if err := db.Database().QueryRow("SELECT preview,raw FROM mail_messages WHERE account_id=? AND mailbox='INBOX' AND uid=2", account).Scan(&stored, &originalRaw); err != nil {
		t.Fatal(err)
	}
	var preview Preview
	if json.Unmarshal([]byte(stored), &preview) != nil {
		t.Fatal("invalid fixture")
	}
	originalHash, originalID := preview.ContentHash, preview.MessageID
	preview.ThreadingVersion = 0
	preview.References = []string{"uncached@example.test"}
	preview.Text = "Preserve this existing cached preview; do not decode MIME again"
	legacy, _ := json.Marshal(preview)
	if _, err := db.Database().Exec("UPDATE mail_messages SET preview=? WHERE account_id=? AND mailbox='INBOX' AND uid=2", string(legacy), account); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Database().Exec("CREATE TABLE qa_thread_updates(value INTEGER); INSERT INTO qa_thread_updates VALUES(0); CREATE TRIGGER qa_thread_repair AFTER UPDATE OF preview ON mail_messages WHEN NEW.uid=2 BEGIN UPDATE qa_thread_updates SET value=value+1; END;"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Messages(ctx, other, account, "INBOX"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("foreign cached repair permitted", err)
	}
	downloads, opens := reader.downloads, reader.opens
	messages, err := service.Messages(ctx, owner, account, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[1].Preview.Text != preview.Text || messages[1].Preview.ContentHash != originalHash || messages[1].Preview.MessageID != originalID || strings.Join(messages[1].Preview.References, ",") != "uncached@example.test,m1@example.test" {
		t.Fatal("cached repair changed identity/body or lost direct parent")
	}
	var rawAfter []byte
	var updated string
	if err = db.Database().QueryRow("SELECT preview,raw FROM mail_messages WHERE account_id=? AND mailbox='INBOX' AND uid=2", account).Scan(&updated, &rawAfter); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(originalRaw, rawAfter) || !strings.Contains(updated, `"ThreadingVersion":1`) {
		t.Fatal("raw MIME altered or metadata not persisted")
	}
	if _, err = service.Messages(ctx, owner, account, "INBOX"); err != nil {
		t.Fatal(err)
	}
	var writes int
	if err = db.Database().QueryRow("SELECT value FROM qa_thread_updates").Scan(&writes); err != nil || writes != 1 {
		t.Fatal("current metadata rewritten", writes, err)
	}
	if reader.downloads != downloads || reader.opens != opens {
		t.Fatal("header repair contacted provider")
	}
}

func TestThreadRepairCASRejectsStaleSourceAndForeignOwner(t *testing.T) {
	for _, change := range []string{"owner", "revision", "validity", "preview", "headers"} {
		t.Run(change, func(t *testing.T) {
			service, _, db, owner, other, account, reader := setup(t)
			ctx := context.Background()
			reader.raw[2] = synthetic(2, "References: <older@example.test>\r\nIn-Reply-To: <m1@example.test>\r\n")
			if err := service.Sync(ctx, owner, account); err != nil {
				t.Fatal(err)
			}
			var original string
			var prefix []byte
			var revision int64
			query := `SELECT m.preview,` + threadingPrefixSQL + `,c.revision FROM mail_messages m JOIN mail_connections c ON c.account_id=m.account_id WHERE m.account_id=? AND m.mailbox='INBOX' AND m.uid=2`
			if err := db.Database().QueryRow(query, account).Scan(&original, &prefix, &revision); err != nil {
				t.Fatal(err)
			}
			var preview Preview
			json.Unmarshal([]byte(original), &preview)
			preview.ThreadingVersion = 0
			preview.References = []string{"older@example.test"}
			old, _ := json.Marshal(preview)
			original = string(old)
			if _, err := db.Database().Exec("UPDATE mail_messages SET preview=? WHERE account_id=? AND uid=2", original, account); err != nil {
				t.Fatal(err)
			}
			_, updated, ok := repairedPreview(preview, prefix)
			if !ok {
				t.Fatal("invalid prefix")
			}
			target := owner
			switch change {
			case "owner":
				target = other
			case "revision":
				db.Database().Exec("UPDATE mail_connections SET revision=revision+1 WHERE account_id=?", account)
			case "validity":
				db.Database().Exec("UPDATE mailboxes SET uid_validity=2 WHERE account_id=? AND name='INBOX'", account)
			case "preview":
				preview.Text = "Newer preview wins"
				raw, _ := json.Marshal(preview)
				db.Database().Exec("UPDATE mail_messages SET preview=? WHERE account_id=? AND uid=2", string(raw), account)
			case "headers":
				db.Database().Exec("UPDATE mail_messages SET raw=? WHERE account_id=? AND uid=2", bytes.Replace(reader.raw[2], []byte("Topic 2"), []byte("New topic"), 1), account)
			}
			var before string
			db.Database().QueryRow("SELECT preview FROM mail_messages WHERE account_id=? AND uid=2", account).Scan(&before)
			err := service.repairThreading(ctx, target, account, "INBOX", []threadingRepair{{uid: 2, validity: 1, revision: revision, original: original, updated: updated, prefix: prefix}})
			if !errors.Is(err, ErrChanged) {
				t.Fatal("stale/foreign repair accepted", err)
			}
			var after string
			db.Database().QueryRow("SELECT preview FROM mail_messages WHERE account_id=? AND uid=2", account).Scan(&after)
			if before != after {
				t.Fatal("newer/foreign preview overwritten")
			}
		})
	}
}

func TestLegacyConversationsScopeReferencesByAccountFolderAndValidity(t *testing.T) {
	message := func(account, folder, id string, validity, uid uint32, refs ...string) CachedMessage {
		return CachedMessage{AccountID: account, Mailbox: folder, Validity: validity, UID: uid, Preview: Preview{MessageID: id, References: refs, Subject: "Siema"}}
	}
	messages := []CachedMessage{message("one", "INBOX", "root", 1, 1), message("one", "INBOX", "reply", 1, 2, "root"), message("two", "INBOX", "foreign", 1, 1, "root"), message("one", "Archive", "other-folder", 1, 1, "root"), message("one", "INBOX", "old-generation", 2, 1, "root")}
	if groups := Conversations(messages); len(groups) != 4 {
		t.Fatal("legacy references crossed source scope", len(groups))
	}
	messages = []CachedMessage{message("one", "INBOX", "a", 1, 1, "missing"), message("one", "INBOX", "b", 1, 2, "missing")}
	if groups := Conversations(messages); len(groups) != 1 || len(groups[0].Messages) != 2 {
		t.Fatal("explicit shared missing reference not grouped")
	}
}

func TestHeaderRepairFillsRecipientNamesWithoutReadingBodies(t *testing.T) {
	raw := []byte("From: alice@example.test\r\nTo: demo@example.test\r\nCc: Third Person <third@example.test>\r\nSubject: Names\r\n\r\nSYNTHETIC-BODY-MUST-NOT-LEAK")
	prefix := raw[:bytes.Index(raw, []byte("\r\n\r\n"))+4]
	legacy := Preview{ThreadingVersion: threadingVersion, ContentHash: "hash", Status: "ready", Subject: "Names", To: []string{"demo@example.test"}}
	repaired, encoded, ok := repairedPreview(legacy, prefix)
	if !ok || repaired.RecipientsVersion != recipientsVersion || len(repaired.ToRecipients) != 1 || repaired.ToRecipients[0].Address != "demo@example.test" {
		t.Fatalf("recipient repair failed: %#v", repaired)
	}
	if len(repaired.CcRecipients) != 1 || repaired.CcRecipients[0].Name != "Third Person" {
		t.Fatalf("cc repair failed: %#v", repaired.CcRecipients)
	}
	if strings.Contains(encoded, "SYNTHETIC-BODY-MUST-NOT-LEAK") || strings.Contains(encoded, "hidden") {
		t.Fatal("header repair read a body or invented a recipient")
	}
	if _, _, ok = repairedPreview(repaired, prefix); ok {
		t.Fatal("repair is not idempotent")
	}
	if _, _, ok = repairedPreview(legacy, []byte("Subject: no terminator")); ok {
		t.Fatal("repair accepted an incomplete header prefix")
	}
}
