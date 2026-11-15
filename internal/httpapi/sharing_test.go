// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mailnet"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type sharingChecker struct{}

func (sharingChecker) CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error {
	return nil
}
func (sharingChecker) CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error {
	return nil
}

func TestSharingPanelIsOwnerOnlyAndRevocable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := identity.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	alice, err := store.CreateUser(ctx, "alice@example.test", "synthetic long password")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.CreateUser(ctx, "bob@example.test", "synthetic long password")
	if err != nil {
		t.Fatal(err)
	}
	token, session, err := store.NewSession(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, otherSession, err := store.NewSession(ctx, bob.ID)
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
	mailAccounts, err := accounts.Open(store.Database(), filepath.Join(dir, "keys"), sharingChecker{})
	if err != nil {
		t.Fatal(err)
	}
	account, err := mailAccounts.Save(ctx, alice.ID, "", accounts.Input{Name: "Work mailbox", Address: "alice@example.test", SentMailbox: "Sent", IMAP: mailnet.Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}, SMTP: mailnet.Endpoint{Host: "smtp.example.test", Port: 465, TLS: "tls"}, IMAPLogin: mailnet.Login{Username: "alice", Password: "synthetic"}, SMTPLogin: mailnet.Login{Username: "alice", Password: "synthetic"}})
	if err != nil {
		t.Fatal(err)
	}
	syncer := mailsync.New(store.Database(), mailAccounts, nil)
	outgoing := outbox.New(store.Database(), mailAccounts, syncer, nil)
	handler, err := NewPersonal(dir, store, AuthConfig{Outbox: outgoing, Origin: "https://mail.example.test", Accounts: mailAccounts, Sync: syncer, MailEndpointsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	get := func(cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/sharing", nil)
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	post := func(cookie, target, csrf string, values url.Values) *httptest.ResponseRecorder {
		values.Set("csrf", csrf)
		req := httptest.NewRequest("POST", target, strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://mail.example.test")
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	page := get(token)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Active grants") || !strings.Contains(page.Body.String(), "Work mailbox") || !strings.Contains(page.Body.String(), "Northstar") {
		t.Fatal("owner sharing panel unavailable", page.Code)
	}
	if granteePanel := get(other); granteePanel.Code != 200 || strings.Contains(granteePanel.Body.String(), "Work mailbox") {
		t.Fatal("grantee panel leaked the owner's account")
	}
	form := func() url.Values {
		return url.Values{"workspace": {workspace.ID}, "account": {account.ID}, "mailbox": {""}, "grantee": {bob.Email}, "can_download": {"1"}}
	}
	if res := post(token, "/sharing", "", form()); res.Code != 403 {
		t.Fatal("grant creation bypassed CSRF", res.Code)
	}
	if res := post(token, "/sharing", session.CSRF, form()); res.Code != 303 || !strings.Contains(res.Header().Get("Location"), "notice=shared") {
		t.Fatal("grant creation failed", res.Code, res.Header().Get("Location"))
	}
	unknown := form()
	unknown.Set("grantee", "missing@example.test")
	if res := post(token, "/sharing", session.CSRF, unknown); !strings.Contains(res.Header().Get("Location"), "error=member") {
		t.Fatal("unknown grantee feedback differed", res.Header().Get("Location"))
	}
	self := form()
	self.Set("grantee", alice.Email)
	if res := post(token, "/sharing", session.CSRF, self); !strings.Contains(res.Header().Get("Location"), "error=input") {
		t.Fatal("self-grant accepted", res.Header().Get("Location"))
	}
	if after := get(token).Body.String(); !strings.Contains(after, bob.Email) || !strings.Contains(after, "download allowed") {
		t.Fatal("active grant not listed")
	}
	ids := regexp.MustCompile(`action="/sharing/([A-Za-z0-9_-]+)/revoke"`).FindAllStringSubmatch(get(token).Body.String(), -1)
	if len(ids) != 1 {
		t.Fatalf("unexpected grant count %d", len(ids))
	}
	id := ids[0][1]
	if res := post(other, "/sharing/"+id+"/revoke", otherSession.CSRF, url.Values{}); !strings.Contains(res.Header().Get("Location"), "error=missing") {
		t.Fatal("foreign revoke accepted", res.Header().Get("Location"))
	}
	if res := post(token, "/sharing/"+id+"/revoke", session.CSRF, url.Values{}); res.Code != 303 || !strings.Contains(res.Header().Get("Location"), "notice=revoked") {
		t.Fatal("revoke failed", res.Code, res.Header().Get("Location"))
	}
	if remaining := regexp.MustCompile(`/sharing/[A-Za-z0-9_-]+/revoke`).FindAllString(get(token).Body.String(), -1); len(remaining) != 0 {
		t.Fatal("revoked grant still listed")
	}
}
