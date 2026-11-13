// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"context"
	"errors"
	"fmt"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mailnet"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type checker struct{}

func (checker) CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }
func (checker) CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }

type fixtureReader struct {
	boxes     []mailnet.Mailbox
	raw       map[uint32][]byte
	opens     int
	downloads int
	fail      bool
}

func (f *fixtureReader) OpenRead(context.Context, mailnet.Endpoint, mailnet.Login) (mailnet.ReadSession, error) {
	f.opens++
	if f.fail {
		return nil, errors.New("offline")
	}
	return f, nil
}
func (f *fixtureReader) Mailboxes() ([]string, error) {
	names := []string{}
	for _, b := range f.boxes {
		names = append(names, b.Name)
	}
	return names, nil
}
func (f *fixtureReader) Snapshot(name string) (mailnet.Mailbox, error) {
	for _, b := range f.boxes {
		if b.Name == name {
			b.Messages = append([]mailnet.RemoteMessage{}, b.Messages...)
			return b, nil
		}
	}
	return mailnet.Mailbox{}, errors.New("missing")
}
func (f *fixtureReader) MIME(_ string, _, uid uint32) ([]byte, error) {
	f.downloads++
	raw, ok := f.raw[uid]
	if !ok {
		return nil, errors.New("download failed")
	}
	return raw, nil
}
func (f *fixtureReader) Close() error { return nil }
func synthetic(uid int, ref string) []byte {
	return []byte(fmt.Sprintf("From: Alice <alice@example.test>\r\nTo: demo@example.test\r\nMessage-ID: <m%d@example.test>\r\n%sSubject: Topic %d\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nSynthetic body %d", uid, ref, uid, uid))
}
func setup(t *testing.T) (*Service, *accounts.Service, *identity.Store, string, string, string, *fixtureReader) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := identity.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	alice, err := db.CreateUser(ctx, "alice@example.test", "synthetic long password")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := db.CreateUser(ctx, "bob@example.test", "another synthetic password")
	if err != nil {
		t.Fatal(err)
	}
	a, err := accounts.Open(db.Database(), filepath.Join(dir, "keys"), checker{})
	if err != nil {
		t.Fatal(err)
	}
	account, err := a.Save(ctx, alice.ID, "", accountInput())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixtureReader{boxes: []mailnet.Mailbox{{Name: "INBOX", Total: 2, UIDValidity: 1, Messages: []mailnet.RemoteMessage{{UID: 1, Size: 200, Received: time.Unix(100, 0)}, {UID: 2, Size: 200, Received: time.Unix(200, 0)}}}}, raw: map[uint32][]byte{1: synthetic(1, ""), 2: synthetic(2, "References: <m1@example.test>\r\n")}}
	return New(db.Database(), a, f), a, db, alice.ID, bob.ID, account.ID, f
}
func accountInput() accounts.Input {
	return accounts.Input{Name: "Test", Address: "alice@example.test", IMAP: mailnet.Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}, SMTP: mailnet.Endpoint{Host: "smtp.example.test", Port: 465, TLS: "tls"}, IMAPLogin: mailnet.Login{Username: "alice", Password: "synthetic-provider"}, SMTPLogin: mailnet.Login{Username: "alice", Password: "synthetic-provider"}}
}
func TestPersistentSyncIsolatedUIDsFlagsExpungeAndReset(t *testing.T) {
	s, a, db, owner, bob, id, f := setup(t)
	ctx := context.Background()
	if err := s.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	messages, err := s.Messages(ctx, owner, id, "INBOX")
	if err != nil || len(messages) != 2 {
		t.Fatal("missing messages", err)
	}
	if c := Conversations(messages); len(c) != 1 || c[0].ReplyCount() != 1 {
		t.Fatal("references not threaded", c)
	}
	if err = s.Sync(ctx, owner, id); err != nil || f.downloads != 2 {
		t.Fatal("reconnect redownloaded or duplicated", err)
	}
	restarted := New(db.Database(), a, f)
	if err = restarted.Sync(ctx, owner, id); err != nil || f.downloads != 2 {
		t.Fatal("checkpoint did not survive service restart")
	}
	if _, err = s.Raw(ctx, bob, id, "INBOX", 1, 1); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("foreign raw MIME exposed")
	}
	if _, err = s.Raw(ctx, owner, id, "INBOX", 99, 1); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("obsolete raw MIME generation exposed")
	}
	before := f.opens
	if !errors.Is(s.Sync(ctx, bob, id), accounts.ErrNotFound) {
		t.Fatal("cross-owner sync allowed")
	}
	if _, err = s.Messages(ctx, bob, id, "INBOX"); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("cross-owner messages exposed")
	}
	if _, err = s.Folders(ctx, bob, id); !errors.Is(err, accounts.ErrNotFound) {
		t.Fatal("cross-owner folders exposed")
	}
	if f.opens != before {
		t.Fatal("unauthorized request reached network")
	}
	second, err := a.Save(ctx, owner, "", accountInput())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Sync(ctx, owner, second.ID); err != nil {
		t.Fatal(err)
	}
	f.boxes[0].Messages = f.boxes[0].Messages[1:]
	f.boxes[0].Messages[0].Flags = []string{`\Seen`, `\Flagged`}
	f.boxes[0].Total = 1
	if err = s.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	messages, _ = s.Messages(ctx, owner, id, "INBOX")
	if len(messages) != 1 || messages[0].UID != 2 || Conversations(messages)[0].Unread || !Conversations(messages)[0].Starred {
		t.Fatal("flags/expunge not reconciled")
	}
	other, _ := s.Messages(ctx, owner, second.ID, "INBOX")
	if len(other) != 2 {
		t.Fatal("same UIDs in another account were modified")
	}
	f.boxes[0].UIDValidity = 2
	f.raw[2] = synthetic(99, "")
	if err = s.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	messages, _ = s.Messages(ctx, owner, id, "INBOX")
	if len(messages) != 1 || messages[0].Validity != 2 || !strings.Contains(messages[0].Preview.Text, "99") {
		t.Fatal("UIDVALIDITY retained obsolete content")
	}
	f.fail = true
	if s.Sync(ctx, owner, id) == nil {
		t.Fatal("remote failure hidden")
	}
	messages, _ = s.Messages(ctx, owner, id, "INBOX")
	if len(messages) != 1 {
		t.Fatal("failure erased cache")
	}
}
func TestIncrementalBudgetAndConfigGeneration(t *testing.T) {
	s, a, _, owner, _, id, f := setup(t)
	ctx := context.Background()
	f.boxes[0].Messages = nil
	f.boxes[0].Total = 21
	for uid := 1; uid <= 21; uid++ {
		f.raw[uint32(uid)] = synthetic(uid, "")
		f.boxes[0].Messages = append(f.boxes[0].Messages, mailnet.RemoteMessage{UID: uint32(uid), Size: 200, Received: time.Unix(int64(uid), 0)})
	}
	if err := s.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	folders, err := s.Folders(ctx, owner, id)
	if err != nil || folders[0].Pending != 1 || f.downloads != 20 {
		t.Fatal("cycle budget missing", folders, err)
	}
	if err = s.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	folders, _ = s.Folders(ctx, owner, id)
	if folders[0].Pending != 0 || f.downloads != 21 {
		t.Fatal("checkpoint did not resume")
	}
	if _, err = a.Save(ctx, owner, id, accountInput()); err != nil {
		t.Fatal(err)
	}
	messages, _ := s.Messages(ctx, owner, id, "INBOX")
	if len(messages) != 0 {
		t.Fatal("updated endpoint kept old account content")
	}
	if err = s.apply(ctx, owner, id, 1, f.boxes[0], nil, nil); !errors.Is(err, ErrChanged) {
		t.Fatal("obsolete generation could overwrite cache", err)
	}
}
func TestFailedDownloadDoesNotCommitPartialMailbox(t *testing.T) {
	s, _, _, owner, _, id, f := setup(t)
	delete(f.raw, 1)
	if s.Sync(context.Background(), owner, id) == nil {
		t.Fatal("missing body accepted")
	}
	m, _ := s.Messages(context.Background(), owner, id, "INBOX")
	if len(m) != 0 {
		t.Fatal("partial mailbox committed")
	}
}

type cancellingReader struct{ entered chan struct{} }

func (r *cancellingReader) OpenRead(ctx context.Context, _ mailnet.Endpoint, _ mailnet.Login) (mailnet.ReadSession, error) {
	close(r.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestWorkerCancellationAndBoundedOnboardingQueue(t *testing.T) {
	s, _, _, _, _, _, _ := setup(t)
	r := &cancellingReader{entered: make(chan struct{})}
	s.reader = r
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	select {
	case <-r.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not load existing account")
	}
	for i := 0; i < 1000; i++ {
		s.Schedule("owner", "account")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not cancel network before storage shutdown")
	}
}
