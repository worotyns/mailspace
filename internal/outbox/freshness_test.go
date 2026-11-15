// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"mailspace.local/mailspace/internal/mailsync"
)

func TestReplySourceChangedAfterResolutionCannotQueue(t *testing.T) {
	for _, mode := range []string{"plain", "signed"} {
		t.Run(mode, func(t *testing.T) { testReplySourceFreshness(t, mode) })
	}
}

func testReplySourceFreshness(t *testing.T, mode string) {
	for _, change := range []string{"imap-current", "imap-validity", "imap-deleted", "outbox-current", "outbox-revision", "outbox-deleted"} {
		t.Run(change, func(t *testing.T) {
			s, a, db, sender, owner, _, account := setup(t)
			ctx := context.Background()
			config, err := a.Get(ctx, owner, account)
			if err != nil {
				t.Fatal(err)
			}
			d := draft()
			if mode == "signed" {
				d = signedDraft(t)
			}
			d.SourceRevision = config.Revision
			var parent *mailsync.Preview
			var mutation string
			var args []any
			if change == "imap-current" || change == "imap-validity" || change == "imap-deleted" {
				raw := []byte("From: sender@example.test\r\nTo: alice@example.test\r\nSubject: Parent\r\nMessage-ID: <parent@example.test>\r\nContent-Type: text/plain\r\n\r\nSynthetic parent")
				encoded, _ := json.Marshal(mailsync.Parse(raw))
				if _, err = db.Database().Exec("INSERT INTO mailboxes(account_id,name,uid_validity,total,synced_at) VALUES(?,'INBOX',7,1,0)", account); err != nil {
					t.Fatal(err)
				}
				if _, err = db.Database().Exec("INSERT INTO mail_messages(account_id,mailbox,uid,flags,size,received_at,raw,preview) VALUES(?,'INBOX',1,'[]',?,0,?,?)", account, len(raw), raw, string(encoded)); err != nil {
					t.Fatal(err)
				}
				d.ReplyFolder, d.ReplyValidity, d.ReplyUID = "INBOX", 7, 1
				parent, err = s.Parent(ctx, owner, account, "INBOX", 7, 1)
				mutation = "UPDATE mailboxes SET uid_validity=8 WHERE account_id=?"
				if change == "imap-deleted" {
					mutation = "DELETE FROM mail_messages WHERE account_id=?"
				}
				args = []any{account}
			} else {
				original, e := s.Queue(ctx, owner, account, nonce, draft())
				if e != nil {
					t.Fatal(e)
				}
				source, e := s.RawSource(ctx, owner, account, original.ID)
				if e != nil {
					t.Fatal(e)
				}
				parsed := mailsync.Parse(source.Raw)
				parent = &parsed
				d.ReplyOutgoing = original.ID
				mutation = "UPDATE outbox SET revision=revision+1 WHERE id=?"
				if change == "outbox-deleted" {
					mutation = "DELETE FROM outbox WHERE id=?"
				}
				args = []any{original.ID}
			}
			if err != nil {
				t.Fatal(err)
			}
			// Queue's resolution completed; reset its source before the write transaction.
			if change == "imap-current" || change == "outbox-current" {
				if _, err = s.queueResolved(ctx, owner, account, "SYNTHETIC_CHILD_NONCE_123456789", d, config, parent); err != nil {
					t.Fatalf("current original rejected: %v", err)
				}
				return
			}
			if _, err = db.Database().Exec(mutation, args...); err != nil {
				t.Fatal(err)
			}
			if _, err = s.queueResolved(ctx, owner, account, "SYNTHETIC_CHILD_NONCE_123456789", d, config, parent); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale original queued: %v", err)
			}
			var count int
			if err = db.Database().QueryRow("SELECT count(*) FROM outbox WHERE nonce=?", "SYNTHETIC_CHILD_NONCE_123456789").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 || sender.calls.Load() != 0 || sender.copies.Load() != 0 {
				t.Fatal("stale reply wrote or sent mail")
			}
		})
	}
}
