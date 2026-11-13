// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"context"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mailnet"
	"mailspace.local/mailspace/internal/mailsync"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

type checker struct{}

func (checker) CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }
func (checker) CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }

type fakeSender struct {
	calls    atomic.Int32
	copies   atomic.Int32
	outcome  mailnet.SendResult
	copyFail bool
}

func (f *fakeSender) Send(context.Context, mailnet.Endpoint, mailnet.Login, string, []string, []byte) mailnet.SendResult {
	f.calls.Add(1)
	return f.outcome
}
func (f *fakeSender) AppendSent(context.Context, mailnet.Endpoint, mailnet.Login, string, []byte) error {
	f.copies.Add(1)
	if f.copyFail {
		return errors.New("synthetic lost APPEND response")
	}
	return nil
}
func setup(t *testing.T) (*Service, *accounts.Service, *identity.Store, *fakeSender, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := identity.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	alice, err := db.CreateUser(ctx, "alice@example.test", "synthetic app password")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := db.CreateUser(ctx, "bob@example.test", "another synthetic app password")
	if err != nil {
		t.Fatal(err)
	}
	a, err := accounts.Open(db.Database(), filepath.Join(dir, "keys"), checker{})
	if err != nil {
		t.Fatal(err)
	}
	config, err := a.Save(ctx, alice.ID, "", accountInput())
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{outcome: mailnet.Accepted}
	m := mailsync.New(db.Database(), a, nil)
	return New(db.Database(), a, m, sender), a, db, sender, alice.ID, bob.ID, config.ID
}
func accountInput() accounts.Input {
	return accounts.Input{Name: "Synthetic", Address: "alice@example.test", IMAP: mailnet.Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}, SMTP: mailnet.Endpoint{Host: "smtp.example.test", Port: 465, TLS: "tls"}, IMAPLogin: mailnet.Login{Username: "alice", Password: "synthetic"}, SMTPLogin: mailnet.Login{Username: "alice", Password: "synthetic"}}
}
func draft() Draft {
	return Draft{To: "recipient@example.test", BCC: "hidden@example.test", Subject: "Synthetic test", Body: "No actual recipient is contacted.", AllowPlaintext: true}
}

const nonce = "SYNTHETICFORMREQUEST123456789"

func TestOneSendForRepeatedConcurrentDispatchAndNonceReplay(t *testing.T) {
	s, _, _, sender, owner, bob, account := setup(t)
	ctx := context.Background()
	r, err := s.Queue(ctx, owner, account, nonce, draft())
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.Queue(ctx, owner, account, nonce, draft())
	if err != nil || same.ID != r.ID {
		t.Fatal("replay created second message", err)
	}
	modified := draft()
	modified.Body = "Different message"
	if _, err = s.Queue(ctx, owner, account, nonce, modified); !errors.Is(err, ErrConflict) {
		t.Fatal("nonce reused with altered content")
	}
	if err = s.Dispatch(ctx, bob, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign user sent mail")
	}
	if _, err = s.Get(ctx, bob, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign user read outbox")
	}
	if _, err = s.List(ctx, bob, account); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("foreign user listed outbox")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Dispatch(ctx, owner, r.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if sender.calls.Load() != 1 || sender.copies.Load() != 1 {
		t.Fatal("duplicate SMTP or APPEND", sender.calls.Load(), sender.copies.Load())
	}
	r, err = s.Get(ctx, owner, r.ID)
	if err != nil || r.State != "accepted" || r.CopyState != "saved" {
		t.Fatal("acceptance not recorded", r, err)
	}
}
func TestUnknownAndFailedSentCopyNeverResend(t *testing.T) {
	for _, outcome := range []mailnet.SendResult{mailnet.Unknown, mailnet.Rejected, mailnet.Accepted} {
		t.Run(string(outcome), func(t *testing.T) {
			s, _, _, sender, owner, _, account := setup(t)
			sender.outcome = outcome
			sender.copyFail = true
			ctx := context.Background()
			r, err := s.Queue(ctx, owner, account, nonce, draft())
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Dispatch(ctx, owner, r.ID); err != nil {
				t.Fatal(err)
			}
			if err = s.Dispatch(ctx, owner, r.ID); err != nil {
				t.Fatal(err)
			}
			r, _ = s.Get(ctx, owner, r.ID)
			if r.State != string(outcome) || sender.calls.Load() != 1 {
				t.Fatal("outcome hidden or message resent", r)
			}
			if outcome == mailnet.Accepted && (r.CopyState != "unknown" || sender.copies.Load() != 1) {
				t.Fatal("lost APPEND response retried or SMTP rolled back", r)
			}
			if outcome != mailnet.Accepted && sender.copies.Load() != 0 {
				t.Fatal("unaccepted message appended to Sent")
			}
		})
	}
}
func TestRecoveryAndChangedConfigurationCannotSendOldMessage(t *testing.T) {
	s, a, db, sender, owner, _, account := setup(t)
	ctx := context.Background()
	r, err := s.Queue(ctx, owner, account, nonce, draft())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Save(ctx, owner, account, accountInput()); err != nil {
		t.Fatal(err)
	}
	if err = s.Dispatch(ctx, owner, r.ID); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Get(ctx, owner, r.ID)
	if sender.calls.Load() != 0 || r.State != "rejected" {
		t.Fatal("old account generation used to send", r)
	}
	r, err = s.Queue(ctx, owner, account, nonce+"2", draft())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Database().Exec("UPDATE outbox SET state='sending',copy_state='copying' WHERE id=?", r.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Dispatch(ctx, owner, r.ID); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Get(ctx, owner, r.ID)
	if r.State != "unknown" || r.CopyState != "unknown" || sender.calls.Load() != 0 {
		t.Fatal("crash recovery resent potentially accepted message", r)
	}
}
