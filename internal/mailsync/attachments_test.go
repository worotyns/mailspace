// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"context"
	"testing"
)

func TestAttachmentListEnumeratesCachedRefsOnly(t *testing.T) {
	s, _, db, owner, _, id, _ := setup(t)
	ctx := context.Background()
	if err := s.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	cacheSearchPreview(t, db, id, 1, Preview{ContentHash: "a", Status: "ready", Subject: "Invoice", Sender: "billing@example.test", Name: "Billing", Attachments: []Attachment{{Name: "invoice.pdf", Type: "application/pdf"}, {Name: "note.txt", Type: "text/plain"}}})
	cacheSearchPreview(t, db, id, 2, Preview{ContentHash: "b", Status: "ready", Subject: "Photos", Sender: "friend@example.test", Attachments: []Attachment{{Name: "photo.png", Type: "image/png"}}})
	cacheSearchPreview(t, db, id, 3, Preview{ContentHash: "c", Status: "ready", Encrypted: true, Subject: "Cipher"})
	items, err := s.AttachmentList(ctx, []string{id}, 0)
	if err != nil || len(items) != 3 {
		t.Fatalf("attachment list %d %v", len(items), err)
	}
	if items[0].Name != "photo.png" || items[0].UID != 2 || items[0].Subject != "Photos" || items[0].Sender != "friend@example.test" {
		t.Fatalf("newest attachment first expected: %#v", items[0])
	}
	if items[1].Name != "invoice.pdf" || items[2].Name != "note.txt" {
		t.Fatalf("attachment order within a message changed: %#v", items[1:])
	}
	for _, item := range items {
		if item.UID == 3 {
			t.Fatal("encrypted attachment listed")
		}
	}
	if foreign, err := s.AttachmentList(ctx, []string{"foreign-account"}, 0); err != nil || len(foreign) != 0 {
		t.Fatalf("scope widened: %d %v", len(foreign), err)
	}
	if limited, err := s.AttachmentList(ctx, []string{id}, 2); err != nil || len(limited) != 2 {
		t.Fatalf("result limit ignored: %d %v", len(limited), err)
	}
	// A structurally wrong cached attachment array is skipped, never fatal.
	cacheSearchPreview(t, db, id, 4, Preview{ContentHash: "d", Status: "ready", Subject: "Broken", Attachments: []Attachment{{Name: "x", Type: "y"}}})
	if _, err = db.Database().ExecContext(ctx, `UPDATE mail_messages SET preview=json_set(preview,'$.Attachments',json_array('oops')) WHERE account_id=? AND mailbox='INBOX' AND uid=4`, id); err != nil {
		t.Fatal(err)
	}
	if items, err = s.AttachmentList(ctx, []string{id}, 0); err != nil || len(items) != 3 {
		t.Fatalf("malformed attachments were not skipped: %d %v", len(items), err)
	}
}
