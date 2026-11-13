// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mailnet"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

type httpMailChecker struct {
	calls int
	fail  bool
}

func (c *httpMailChecker) CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error {
	c.calls++
	if c.fail {
		return mailnet.CheckError{Protocol: "IMAP", Stage: "authentication"}
	}
	return nil
}
func (c *httpMailChecker) CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error {
	c.calls++
	return nil
}
func TestMailAccountFormsNeverEchoPasswordsOrCrossOwners(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := identity.Open(filepath.Join(dir, "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	alice, err := store.CreateUser(ctx, "alice@example.test", "long synthetic app password")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.CreateUser(ctx, "bob@example.test", "another synthetic password")
	if err != nil {
		t.Fatal(err)
	}
	token, session, err := store.NewSession(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	bobToken, bobSession, err := store.NewSession(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	checker := &httpMailChecker{}
	service, err := accounts.Open(store.Database(), filepath.Join(dir, "keys"), checker)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPersonal(dir, store, AuthConfig{Origin: "https://mail.example.test", Accounts: service, MailEndpointsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, values url.Values, cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "https://mail.example.test"+path, strings.NewReader(values.Encode()))
		req.Header.Set("Origin", "https://mail.example.test")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	values := url.Values{"csrf": {session.CSRF}, "name": {"Work"}, "address": {"alice@example.test"}, "imap_host": {"imap.example.test"}, "imap_port": {"993"}, "imap_tls": {"tls"}, "imap_user": {"private-login"}, "imap_password": {"MUST-NOT-APPEAR-IN-HTML"}, "smtp_host": {"smtp.example.test"}, "smtp_port": {"465"}, "smtp_tls": {"tls"}, "same_credentials": {"1"}}
	if response := request("POST", "/accounts", url.Values{}, token); response.Code != 403 {
		t.Fatal("account write bypassed CSRF")
	}
	home := request("GET", "/", nil, token)
	if home.Code != 200 || !strings.Contains(home.Body.String(), "Connect mail account") {
		t.Fatal("personal home does not expose account setup")
	}
	response := request("GET", "/accounts/new", nil, token)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Connect your mail") {
		t.Fatal("setup page missing")
	}
	response = request("POST", "/accounts", values, token)
	if response.Code != 303 {
		t.Fatalf("account setup failed %d %s", response.Code, response.Body.String())
	}
	list, err := store.Accounts(ctx, alice.ID)
	if err != nil || len(list) != 1 {
		t.Fatal("account not saved")
	}
	id := list[0].ID
	response = request("GET", "/accounts/"+id+"/edit", nil, token)
	if response.Code != 200 || strings.Contains(response.Body.String(), values.Get("imap_password")) || !strings.Contains(response.Body.String(), "private-login") {
		t.Fatal("saved password exposed or edit missing")
	}
	before := checker.calls
	response = request("GET", "/accounts/"+id+"/edit", nil, bobToken)
	if response.Code != 404 {
		t.Fatal("cross-user configuration leak")
	}
	values.Set("csrf", bobSession.CSRF)
	response = request("POST", "/accounts/"+id, values, bobToken)
	if response.Code != 404 || checker.calls != before {
		t.Fatal("cross-user update reached connector")
	}
	values.Set("csrf", session.CSRF)
	checker.fail = true
	response = request("POST", "/accounts", values, token)
	if response.Code != 422 || strings.Contains(response.Body.String(), values.Get("imap_password")) {
		t.Fatal("failed probe echoed secret")
	}
	values.Set("imap_tls", "none")
	response = request("POST", "/accounts", values, token)
	if response.Code != 400 {
		t.Fatal("plaintext mode accepted")
	}
	values.Set("imap_tls", "tls")
	values.Set("name", "<script>alert(1)</script>")
	response = request("POST", "/accounts", values, token)
	if strings.Contains(response.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("account form reflected active HTML")
	}
}
