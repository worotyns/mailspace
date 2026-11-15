// SPDX-License-Identifier: AGPL-3.0-only
package sharing

import (
	"context"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mailnet"
	"path/filepath"
	"testing"
)

type checker struct{}

func (checker) CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }
func (checker) CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }

func setup(t *testing.T) (*Service, *identity.Store, *accounts.Service, identity.User, identity.User, identity.User, identity.Workspace, accounts.Config) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := identity.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	alice, err := store.CreateUser(ctx, "alice@example.test", "synthetic long password")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.CreateUser(ctx, "bob@example.test", "synthetic long password")
	if err != nil {
		t.Fatal(err)
	}
	carol, err := store.CreateUser(ctx, "carol@example.test", "synthetic long password")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := store.CreateWorkspace(ctx, alice.ID, "Northstar")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.AddMember(ctx, alice.ID, workspace.ID, bob.Email); err != nil {
		t.Fatal(err)
	}
	accountService, err := accounts.Open(store.Database(), filepath.Join(dir, "keys"), checker{})
	if err != nil {
		t.Fatal(err)
	}
	account, err := accountService.Save(ctx, alice.ID, "", accounts.Input{Name: "Northstar mailbox", Address: "alice@example.test", SentMailbox: "Sent", IMAP: mailnet.Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}, SMTP: mailnet.Endpoint{Host: "smtp.example.test", Port: 465, TLS: "tls"}, IMAPLogin: mailnet.Login{Username: "alice@example.test", Password: "provider password"}, SMTPLogin: mailnet.Login{Username: "alice@example.test", Password: "provider password"}})
	if err != nil {
		t.Fatal(err)
	}
	return New(store, accountService), store, accountService, alice, bob, carol, workspace, account
}

func TestGrantAuthorizeRevokeTwoUserBoundaries(t *testing.T) {
	svc, _, accountService, alice, bob, carol, workspace, account := setup(t)
	ctx := context.Background()
	// Only the account owner may grant, and only to a current member.
	if _, err := svc.Grant(ctx, bob.ID, workspace.ID, account.ID, "", alice.Email, true); !errors.Is(err, ErrForbidden) {
		t.Fatal("foreign grantor accepted", err)
	}
	if _, err := svc.Grant(ctx, alice.ID, workspace.ID, account.ID, "", carol.Email, true); !errors.Is(err, ErrForbidden) {
		t.Fatal("non-member grantee accepted", err)
	}
	if _, err := svc.Grant(ctx, alice.ID, workspace.ID, account.ID, "", alice.Email, true); !errors.Is(err, ErrInput) {
		t.Fatal("self grant accepted", err)
	}
	if _, err := svc.Grant(ctx, alice.ID, workspace.ID, account.ID, "INBOX\x00", bob.Email, true); !errors.Is(err, ErrInput) {
		t.Fatal("invalid mailbox accepted", err)
	}
	if _, err := svc.Grant(ctx, alice.ID, "missing-workspace", account.ID, "", bob.Email, true); !errors.Is(err, ErrForbidden) {
		t.Fatal("unknown workspace accepted", err)
	}
	if _, err := svc.Grant(ctx, alice.ID, workspace.ID, account.ID, "", "missing@example.test", true); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown grantee accepted", err)
	}
	grant, err := svc.Grant(ctx, alice.ID, workspace.ID, account.ID, "", bob.Email, false)
	if err != nil || grant.GranteeEmail != bob.Email || grant.GrantorEmail != alice.Email || grant.Mailbox != "" || grant.CanDownload {
		t.Fatal("valid grant failed", grant, err)
	}
	if ok, err := svc.Authorized(ctx, bob.ID, account.ID, "INBOX"); err != nil || !ok {
		t.Fatal("grantee not authorized", ok, err)
	}
	if ok, _ := svc.Authorized(ctx, carol.ID, account.ID, "INBOX"); ok {
		t.Fatal("non-grantee authorized")
	}
	if ok, _ := svc.Authorized(ctx, bob.ID, "other-account", "INBOX"); ok {
		t.Fatal("unknown account authorized")
	}
	if ok, err := svc.Authorized(ctx, bob.ID, account.ID, "Archive"); err != nil || !ok {
		t.Fatal("all-folder grant did not cover another folder", ok, err)
	}
	// The grantee sees the grant; the management list stays owner-only.
	shared, err := svc.GrantedAccounts(ctx, bob.ID)
	if err != nil || len(shared) != 1 || shared[0].GrantorEmail != alice.Email {
		t.Fatal("grantee view incorrect", shared, err)
	}
	if list, err := svc.List(ctx, alice.ID); err != nil || len(list) != 1 {
		t.Fatal("owner list incorrect", list, err)
	}
	if list, err := svc.List(ctx, bob.ID); err != nil || len(list) != 0 {
		t.Fatal("another user's grants leaked", list, err)
	}
	if err = svc.Revoke(ctx, bob.ID, grant.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign revoke accepted", err)
	}
	if err = svc.Revoke(ctx, alice.ID, "short"); !errors.Is(err, ErrInput) {
		t.Fatal("malformed grant id accepted", err)
	}
	if err = svc.Revoke(ctx, alice.ID, grant.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := svc.Authorized(ctx, bob.ID, account.ID, "INBOX"); ok {
		t.Fatal("revocation did not block the next read")
	}
	// A mailbox-scoped grant never widens to another folder.
	second, err := accountService.Save(ctx, alice.ID, "", accounts.Input{Name: "Second", Address: "alice@example.test", SentMailbox: "Sent", IMAP: mailnet.Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}, SMTP: mailnet.Endpoint{Host: "smtp.example.test", Port: 465, TLS: "tls"}, IMAPLogin: mailnet.Login{Username: "alice@example.test", Password: "provider password"}, SMTPLogin: mailnet.Login{Username: "alice@example.test", Password: "provider password"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Grant(ctx, alice.ID, workspace.ID, second.ID, "INBOX", bob.Email, false); err != nil {
		t.Fatal(err)
	}
	if ok, _ := svc.Authorized(ctx, bob.ID, second.ID, "INBOX"); !ok {
		t.Fatal("scoped grant did not authorize the named folder")
	}
	if ok, _ := svc.Authorized(ctx, bob.ID, second.ID, "Archive"); ok {
		t.Fatal("scoped grant widened to another folder")
	}
}

func TestGrantFailsClosedWhenMembershipChanges(t *testing.T) {
	svc, store, _, alice, bob, _, workspace, account := setup(t)
	ctx := context.Background()
	if _, err := svc.Grant(ctx, alice.ID, workspace.ID, account.ID, "", bob.Email, true); err != nil {
		t.Fatal(err)
	}
	if ok, _ := svc.Authorized(ctx, bob.ID, account.ID, "INBOX"); !ok {
		t.Fatal("active grant not authorized")
	}
	// Removing the member revokes access immediately even before a grant delete.
	if err := store.RemoveMember(ctx, alice.ID, workspace.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := svc.Authorized(ctx, bob.ID, account.ID, "INBOX"); ok {
		t.Fatal("removed member still authorized")
	}
	if shared, err := svc.GrantedAccounts(ctx, bob.ID); err != nil || len(shared) != 0 {
		t.Fatal("removed member still sees the grant", shared, err)
	}
	// A grant fails closed when the grantor is removed from the organization by
	// its owner, even though the account owner keeps the account.
	dave, err := store.CreateUser(ctx, "dave@example.test", "synthetic long password")
	if err != nil {
		t.Fatal(err)
	}
	bob2, err := store.CreateUser(ctx, "bob2@example.test", "synthetic long password")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := store.CreateWorkspace(ctx, dave.ID, "Second org")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.AddMember(ctx, dave.ID, fresh.ID, alice.Email); err != nil {
		t.Fatal(err)
	}
	if err = store.AddMember(ctx, dave.ID, fresh.ID, bob2.Email); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Grant(ctx, alice.ID, fresh.ID, account.ID, "", bob2.Email, false); err != nil {
		t.Fatal(err)
	}
	if ok, _ := svc.Authorized(ctx, bob2.ID, account.ID, "INBOX"); !ok {
		t.Fatal("second member not authorized")
	}
	if err = store.RemoveMember(ctx, dave.ID, fresh.ID, alice.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := svc.Authorized(ctx, bob2.ID, account.ID, "INBOX"); ok {
		t.Fatal("grant survived grantor leaving the organization")
	}
}
