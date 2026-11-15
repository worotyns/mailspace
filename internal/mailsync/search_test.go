// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"context"
	"encoding/json"
	"mailspace.local/mailspace/internal/identity"
	"testing"
)

func cacheSearchPreview(t *testing.T, store *identity.Store, account string, uid uint32, preview Preview) {
	t.Helper()
	encoded, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Database().Exec(`INSERT INTO mail_messages(account_id,mailbox,uid,flags,size,received_at,raw,preview) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(account_id,mailbox,uid) DO UPDATE SET preview=excluded.preview`, account, "INBOX", uid, `[]`, int64(len(encoded)), int64(uid), []byte("x"), string(encoded)); err != nil {
		t.Fatal(err)
	}
}

func TestSearchTextMatchesOrdinaryCachedTextOnly(t *testing.T) {
	s, _, db, owner, _, id, _ := setup(t)
	ctx := context.Background()
	if err := s.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	uid1 := SearchKey{AccountID: id, Mailbox: "INBOX", UID: 1}
	uid2 := SearchKey{AccountID: id, Mailbox: "INBOX", UID: 2}
	keys, err := s.SearchText(ctx, []string{id}, "synthetic body")
	if err != nil || len(keys) != 2 {
		t.Fatalf("term search failed: %d %v", len(keys), err)
	}
	if !keys[uid2] {
		t.Fatal("expected uid 2 match")
	}
	if keys, err = s.SearchText(ctx, []string{id}, "body 2"); err != nil || len(keys) != 1 {
		t.Fatalf("all terms must match: %d %v", len(keys), err)
	}
	if keys, err = s.SearchText(ctx, []string{id}, "absentterm"); err != nil || len(keys) != 0 {
		t.Fatalf("unexpected match: %d %v", len(keys), err)
	}
	// Encrypted and empty previews contribute no indexed text.
	cacheSearchPreview(t, db, id, 9, Preview{ContentHash: "enc", Status: "ready", Encrypted: true, Subject: "cipher"})
	if keys, err = s.SearchText(ctx, []string{id}, "cipher"); err != nil || len(keys) != 0 {
		t.Fatalf("encrypted preview searched: %d %v", len(keys), err)
	}
	// Account scope is exactly the caller-supplied ids.
	if keys, err = s.SearchText(ctx, []string{"foreign-account"}, "synthetic"); err != nil || len(keys) != 0 {
		t.Fatalf("scope widened: %d %v", len(keys), err)
	}
	// FTS operators and punctuation stay literal, never syntax.
	for _, query := range []string{`"`, `OR`, `*`, `NEAR(`, `body:`, `synthetic OR absentterm`, `"synthetic body"`} {
		if _, err = s.SearchText(ctx, []string{id}, query); err != nil {
			t.Fatalf("query %q errored: %v", query, err)
		}
	}
	// Triggers keep the index in step with updates and deletes.
	cacheSearchPreview(t, db, id, 1, Preview{ContentHash: "h1", Status: "ready", Text: "replacement keyword"})
	if keys, err = s.SearchText(ctx, []string{id}, "synthetic"); err != nil || keys[uid1] || !keys[uid2] {
		t.Fatalf("update did not reindex exactly one row: %v %v", keys, err)
	}
	if keys, err = s.SearchText(ctx, []string{id}, "replacement"); err != nil || !keys[uid1] {
		t.Fatalf("updated text not indexed: %v %v", keys, err)
	}
	if _, err = db.Database().ExecContext(ctx, "DELETE FROM mail_messages WHERE account_id=? AND mailbox='INBOX' AND uid=1", id); err != nil {
		t.Fatal(err)
	}
	if keys, err = s.SearchText(ctx, []string{id}, "replacement"); err != nil || len(keys) != 0 {
		t.Fatalf("delete left a stale index entry: %v %v", keys, err)
	}
}
