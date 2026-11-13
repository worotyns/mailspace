// SPDX-License-Identifier: AGPL-3.0-only
package collections

import (
	"context"
	"encoding/json"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailnet"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"mailspace.local/mailspace/internal/timeline"
	"path/filepath"
	"testing"
)

type checker struct{}

func (checker) CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }
func (checker) CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }
func setup(t *testing.T) (*Service, *identity.Store, *accounts.Service, string, string, accounts.Config, mail.SourceRef, mail.SourceRef) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := identity.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	owner, err := db.CreateUser(ctx, "owner@example.test", "Synthetic collection password")
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateUser(ctx, "other@example.test", "Synthetic second password")
	if err != nil {
		t.Fatal(err)
	}
	a, err := accounts.Open(db.Database(), filepath.Join(dir, "keys"), checker{})
	if err != nil {
		t.Fatal(err)
	}
	config, err := a.Save(ctx, owner.ID, "", input())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Database().Exec("INSERT INTO mailboxes(account_id,name,uid_validity,total,synced_at) VALUES(?,'INBOX',7,2,0)", config.ID); err != nil {
		t.Fatal(err)
	}
	for uid := 1; uid <= 2; uid++ {
		id := "root@example.test"
		refs := ""
		if uid == 2 {
			id = "reply@example.test"
			refs = "References: <root@example.test>\r\n"
		}
		raw := []byte("From: sender@example.test\r\nTo: owner@example.test\r\nSubject: Original thread\r\nMessage-ID: <" + id + ">\r\n" + refs + "Content-Type: text/plain; charset=utf-8\r\n\r\nSynthetic body")
		encoded, _ := json.Marshal(mailsync.Parse(raw))
		if _, err = db.Database().Exec("INSERT INTO mail_messages(account_id,mailbox,uid,flags,size,received_at,raw,preview) VALUES(?,'INBOX',?,'[]',?,0,?,?)", config.ID, uid, len(raw), raw, string(encoded)); err != nil {
			t.Fatal(err)
		}
	}
	syncer := mailsync.New(db.Database(), a, nil)
	o := outbox.New(db.Database(), a, syncer, nil)
	resolver := timeline.New(db, a, syncer, o)
	ref := mail.SourceRef{Kind: "imap", AccountID: config.ID, Revision: config.Revision, Mailbox: "INBOX", Validity: 7, UID: 1}
	second := ref
	second.UID = 2
	return New(db.Database(), resolver), db, a, owner.ID, other.ID, config, ref, second
}
func input() accounts.Input {
	return accounts.Input{Name: "Synthetic", Address: "owner@example.test", IMAP: mailnet.Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}, SMTP: mailnet.Endpoint{Host: "smtp.example.test", Port: 465, TLS: "tls"}, IMAPLogin: mailnet.Login{Username: "owner", Password: "synthetic"}, SMTPLogin: mailnet.Login{Username: "owner", Password: "synthetic"}}
}
func create(t *testing.T, s *Service, owner, name string) Item {
	t.Helper()
	item, err := s.Create(context.Background(), owner, "space", name, "Editable relevance brief", "lilac")
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func TestSpacesAssignIndividualMessagesManyToManyAndPreserveManualAuthority(t *testing.T) {
	s, db, _, owner, _, _, first, second := setup(t)
	ctx := context.Background()
	payments := create(t, s, owner, "Payments")
	personal := create(t, s, owner, "Personal")
	if err := s.Assign(ctx, owner, "space", payments.ID, first, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign(ctx, owner, "space", personal.ID, first, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign(ctx, owner, "space", personal.ID, second, true); err != nil {
		t.Fatal(err)
	}
	firstID, err := s.resolver.Identity(ctx, owner, first)
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := s.resolver.Identity(ctx, owner, second)
	if err != nil {
		t.Fatal(err)
	}
	memberships, err := s.Memberships(ctx, owner)
	if err != nil || len(memberships[firstID.Key]) != 2 || len(memberships[secondID.Key]) != 1 {
		t.Fatal("Space grouped a whole email thread or lost multi-membership", memberships, err)
	}
	var revision int64
	query := "SELECT revision FROM message_manual_revisions WHERE account_id=? AND account_revision=? AND message_key=?"
	if err = db.Database().QueryRow(query, firstID.AccountID, firstID.Revision, firstID.Key).Scan(&revision); err != nil || revision != 2 {
		t.Fatal("manual revision missing", revision, err)
	}
	if err = s.Assign(ctx, owner, "space", payments.ID, first, true); err != nil {
		t.Fatal(err)
	}
	if err = db.Database().QueryRow(query, firstID.AccountID, firstID.Revision, firstID.Key).Scan(&revision); err != nil || revision != 2 {
		t.Fatal("duplicate assignment changed manual epoch")
	}
	if err = s.Assign(ctx, owner, "space", payments.ID, first, false); err != nil {
		t.Fatal(err)
	}
	memberships, _ = s.Memberships(ctx, owner)
	if len(memberships[firstID.Key]) != 1 || len(memberships[secondID.Key]) != 1 {
		t.Fatal("removal changed another Space/message")
	}
	if err = s.Remove(ctx, owner, "space", personal.ID); err != nil {
		t.Fatal(err)
	}
	memberships, _ = s.Memberships(ctx, owner)
	if len(memberships) != 0 {
		t.Fatal("deleted Space assignments retained")
	}
	if _, err = s.resolver.Identity(ctx, owner, first); err != nil {
		t.Fatal("Space deletion mutated original mail")
	}
}
func TestPrivateSpacesAndLabelsRemainOwnerScopedAndSourcesAreReauthorized(t *testing.T) {
	s, db, a, owner, other, account, first, _ := setup(t)
	ctx := context.Background()
	space := create(t, s, owner, "Private")
	workspace, err := db.CreateWorkspace(ctx, owner, "Shared organization")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AddMember(ctx, owner, workspace.ID, "other@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, other, "space", space.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("organization membership granted private Space")
	}
	if err = s.Assign(ctx, other, "space", space.ID, first, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign assignment accepted")
	}
	otherSpace := create(t, s, other, "Other private")
	if err = s.Assign(ctx, other, "space", otherSpace.ID, first, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign source became visible through own Space")
	}
	label, err := s.Create(ctx, owner, "label", "Important", "", "rose")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Assign(ctx, owner, "label", label.ID, first, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, owner, "space", label.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("label confused with Space")
	}
	if _, err = s.Update(ctx, other, "space", space.ID, "Takeover", "", "lilac", space.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign update accepted")
	}
	changed := first
	changed.Validity++
	if err = s.Assign(ctx, owner, "space", space.ID, changed, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale UID namespace accepted")
	}
	if _, err = a.Save(ctx, owner, account.ID, input()); err != nil {
		t.Fatal(err)
	}
	if err = s.Assign(ctx, owner, "space", space.ID, first, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("reconnected account reused old source")
	}
	memberships, err := s.Memberships(ctx, owner)
	if err != nil || len(memberships) != 0 {
		t.Fatal("old config assignments entered new namespace", err)
	}
}
func TestSpaceDefinitionsAreVersionedAndMembershipWritesAreAtomic(t *testing.T) {
	s, db, _, owner, _, _, first, _ := setup(t)
	ctx := context.Background()
	space := create(t, s, owner, "Payments")
	updated, err := s.Update(ctx, owner, "space", space.ID, "Payments", "Payments, publisher cooperation and the payments market", "blue", space.Revision)
	if err != nil || updated.Revision != 2 {
		t.Fatal("description update not versioned", err)
	}
	if _, err = s.Update(ctx, owner, "space", space.ID, "Stale", "old brief", "rose", space.Revision); !errors.Is(err, ErrChanged) {
		t.Fatal("stale model/description revision overwritten")
	}
	if _, err = s.Create(ctx, owner, "space", "payments", "Duplicate", "green"); !errors.Is(err, ErrInput) {
		t.Fatal("duplicate Space name accepted")
	}
	if _, err = s.Create(ctx, owner, "label", "Invalid", "Labels have no classification brief", "green"); !errors.Is(err, ErrInput) {
		t.Fatal("label accepted Space-only description")
	}
	if _, err = db.Database().Exec(`CREATE TRIGGER synthetic_manual_failure BEFORE INSERT ON message_manual_revisions BEGIN SELECT RAISE(ABORT,'synthetic revision failure'); END`); err != nil {
		t.Fatal(err)
	}
	defer db.Database().Exec("DROP TRIGGER synthetic_manual_failure")
	if err = s.Assign(ctx, owner, "space", space.ID, first, true); err == nil {
		t.Fatal("revision failure was ignored")
	}
	memberships, err := s.Memberships(ctx, owner)
	if err != nil || len(memberships) != 0 {
		t.Fatal("partial assignment committed without manual epoch", err)
	}
}
