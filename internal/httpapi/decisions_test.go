// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/decision"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailnet"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"mailspace.local/mailspace/internal/timeline"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

type classificationChecker struct{}

func (classificationChecker) CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error {
	return nil
}
func (classificationChecker) CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error {
	return nil
}

type classificationProvider struct {
	calls  int
	input  decision.Input
	target string
}

func (p *classificationProvider) Name() string { return "Synthetic HTTP classifier" }
func (p *classificationProvider) Propose(_ context.Context, input decision.Input) (decision.Proposal, error) {
	p.calls++
	p.input = input
	return decision.Proposal{SpaceIDs: []string{p.target}, Confidence: .99}, nil
}
func TestClassificationHTTPConsentScopeAndSelection(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := identity.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	alice, err := db.CreateUser(ctx, "alice@example.test", "Synthetic Alice password")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := db.CreateUser(ctx, "bob@example.test", "Synthetic Bob password")
	if err != nil {
		t.Fatal(err)
	}
	token, session, err := db.NewSession(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, otherSession, err := db.NewSession(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, err := accounts.Open(db.Database(), filepath.Join(dir, "keys"), classificationChecker{})
	if err != nil {
		t.Fatal(err)
	}
	account, err := a.Save(ctx, alice.ID, "", accounts.Input{Name: "Synthetic mailbox", Address: alice.Email, IMAP: mailnet.Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}, SMTP: mailnet.Endpoint{Host: "smtp.example.test", Port: 465, TLS: "tls"}, IMAPLogin: mailnet.Login{Username: "alice", Password: "synthetic"}, SMTPLogin: mailnet.Login{Username: "alice", Password: "synthetic"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Database().Exec("INSERT INTO mailboxes(account_id,name,uid_validity,total,synced_at) VALUES(?,'INBOX',7,1,0)", account.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: sender@example.test\r\nTo: alice@example.test\r\nSubject: PRIVATE_HEADER_CANARY\r\nMessage-ID: <encrypted@example.test>\r\nContent-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=enc\r\n\r\n--enc\r\nContent-Type: application/pgp-encrypted\r\n\r\nVersion: 1\r\n--enc\r\nContent-Type: application/octet-stream\r\n\r\n-----BEGIN PGP MESSAGE-----\r\nSYNTHETIC\r\n-----END PGP MESSAGE-----\r\n--enc--\r\n")
	preview := mailsync.Parse(raw)
	if !preview.Encrypted {
		t.Fatal("fixture must be encrypted")
	}
	encoded, _ := json.Marshal(preview)
	_, err = db.Database().Exec("INSERT INTO mail_messages(account_id,mailbox,uid,flags,size,received_at,raw,preview) VALUES(?,'INBOX',1,'[]',?,1,?,?)", account.ID, len(raw), raw, string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	syncer := mailsync.New(db.Database(), a, nil)
	box := outbox.New(db.Database(), a, syncer, nil)
	sources := timeline.New(db, a, syncer, box)
	groups := collections.New(db.Database(), sources)
	space, err := groups.Create(ctx, alice.ID, "space", "Payments", "Payments relevance", "blue")
	if err != nil {
		t.Fatal(err)
	}
	provider := &classificationProvider{target: space.ID}
	engine := decision.New(db.Database(), sources, groups, provider)
	if err = engine.Save(ctx, bob.ID, "manual", "metadata"); err != nil {
		t.Fatal(err)
	}
	handler, err := NewPersonal(dir, db, AuthConfig{Accounts: a, Sync: syncer, Outbox: box, Decisions: engine, Origin: "https://mail.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, cookie, csrf, content, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Origin", "https://mail.example.test")
		req.Header.Set("Content-Type", content)
		req.Header.Set("X-CSRF-Token", csrf)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	form := func(mode, providerName, consent string) string {
		return url.Values{"mode": {mode}, "provider": {providerName}, "payload": {"metadata"}, "consent": {consent}}.Encode()
	}
	for _, tc := range []struct {
		name, csrf, provider, consent string
		code                          int
	}{
		{"missing csrf", "", provider.Name(), "allow", 403}, {"missing consent", session.CSRF, provider.Name(), "", 400}, {"changed provider", session.CSRF, "Different provider", "allow", 400}, {"valid", session.CSRF, provider.Name(), "allow", 303},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := request("POST", "/automation", token, tc.csrf, "application/x-www-form-urlencoded", form("auto", tc.provider, tc.consent))
			if res.Code != tc.code {
				t.Fatalf("status %d: %s", res.Code, res.Body.String())
			}
		})
	}
	ref := mail.SourceRef{Kind: "imap", AccountID: account.ID, Revision: account.Revision, Mailbox: "INBOX", Validity: 7, UID: 1}
	refJSON, _ := json.Marshal(ref)
	reference := base64.RawURLEncoding.EncodeToString(refJSON)
	reader := request("GET", "/messages/read?"+url.Values{"reference": {reference}}.Encode(), token, "", "", "")
	if reader.Code != 200 || !strings.Contains(reader.Body.String(), "selection-classify-open") || !strings.Contains(reader.Body.String(), "data-classify-provider=") {
		t.Fatal("owned encrypted reader missing explicit classifier")
	}
	foreignReader := request("GET", "/messages/read?"+url.Values{"reference": {reference}}.Encode(), other, "", "", "")
	if foreignReader.Code != 404 {
		t.Fatal("foreign encrypted reader leaked")
	}
	payload := func(approved bool, extra bool) string {
		value := map[string]any{"reference": reference, "excerpt": "EXPLICIT_SELECTION_CANARY", "requestId": "synthetic-selection-request-id", "consent": map[string]any{"provider": provider.Name(), "purpose": "classify-selected-excerpt", "approved": approved}}
		if extra {
			value["privateKey"] = "must be rejected"
		}
		body, _ := json.Marshal(value)
		return string(body)
	}
	for _, tc := range []struct {
		name, cookie, csrf string
		approved, extra    bool
		code               int
	}{
		{"anonymous", "", "", true, false, 401}, {"csrf", token, "", true, false, 403}, {"unknown field", token, session.CSRF, true, true, 400}, {"unapproved", token, session.CSRF, false, false, 422}, {"foreign source", other, otherSession.CSRF, true, false, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := request("POST", "/api/automation/client-selection", tc.cookie, tc.csrf, "application/json", payload(tc.approved, tc.extra))
			if res.Code != tc.code {
				t.Fatalf("status %d: %s", res.Code, res.Body.String())
			}
			if provider.calls != 0 {
				t.Fatal("denied request reached provider")
			}
		})
	}
	res := request("POST", "/api/automation/client-selection", token, session.CSRF, "application/json", payload(true, false))
	if res.Code != 200 {
		t.Fatalf("selection status %d: %s", res.Code, res.Body.String())
	}
	if provider.calls != 1 || provider.input.Text != "EXPLICIT_SELECTION_CANARY" || provider.input.Subject != "" || provider.input.From != "" {
		t.Fatal("wrong selection export")
	}
	var result struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(res.Body.Bytes(), &result) != nil || result.ID == "" {
		t.Fatal("missing run id")
	}
	run, err := engine.Get(ctx, alice.ID, result.ID)
	if err != nil || run.State != "dry-run" {
		t.Fatal("selection auto-applied")
	}
	res = request("GET", "/automation", other, "", "", "")
	if strings.Contains(res.Body.String(), result.ID) {
		t.Fatal("other owner saw trace")
	}
	res = request("POST", "/automation/runs/"+result.ID+"/apply", other, otherSession.CSRF, "application/x-www-form-urlencoded", "")
	if res.Code != 303 || !strings.Contains(res.Header().Get("Location"), "failed") {
		t.Fatal("other owner applied decision")
	}

	bulk := url.Values{"scope": {"selected"}, "run_id": {result.ID, "foreign-or-missing-run"}}.Encode()
	res = request("POST", "/automation/bulk/apply", token, session.CSRF, "application/x-www-form-urlencoded", bulk)
	if res.Code != 404 {
		t.Fatal("mixed foreign selection accepted", res.Code)
	}
	unchanged, _ := engine.Get(ctx, alice.ID, result.ID)
	if unchanged.State != "dry-run" {
		t.Fatal("bulk authorization partially applied own decision")
	}
	bulk = url.Values{"scope": {"selected"}, "run_id": {result.ID, result.ID}}.Encode()
	res = request("POST", "/automation/bulk/apply", token, session.CSRF, "application/x-www-form-urlencoded", bulk)
	if res.Code != 303 || !strings.Contains(res.Header().Get("Location"), "applied=1") {
		t.Fatal("bulk approval failed", res.Code, res.Body.String())
	}
	if provider.calls != 1 {
		t.Fatal("bulk acceptance invoked provider")
	}
	res = request("POST", "/automation/runs/"+result.ID+"/apply", token, session.CSRF, "application/x-www-form-urlencoded", "")
	if res.Code != 303 || !strings.Contains(res.Header().Get("Location"), "applied") {
		t.Fatalf("approval failed: %s", res.Body.String())
	}
	var snapshot, proposal string
	if err = db.Database().QueryRow("SELECT snapshot,decision FROM decision_runs WHERE id=?", result.ID).Scan(&snapshot, &proposal); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshot+proposal, "CANARY") {
		t.Fatal("trace retained private content")
	}
	if provider.calls != 1 {
		t.Fatal("approval re-exported content")
	}
	t.Run("local rules consent and ownership", func(t *testing.T) {
		form := url.Values{"kind": {"sender"}, "value": {"sender@example.test"}, "space": {space.ID}}.Encode()
		if request("POST", "/rules", token, "", "application/x-www-form-urlencoded", form).Code != 403 {
			t.Fatal("rule creation bypassed CSRF")
		}
		if request("POST", "/rules", other, otherSession.CSRF, "application/x-www-form-urlencoded", form).Code != 422 {
			t.Fatal("rule targeted a foreign Space")
		}
		if response := request("POST", "/rules", token, session.CSRF, "application/x-www-form-urlencoded", form); response.Code != 303 {
			t.Fatalf("rule creation failed: %s", response.Body.String())
		}
		var ruleID string
		var enabled bool
		if err := db.Database().QueryRow("SELECT id,enabled FROM message_rules WHERE owner_id=?", alice.ID).Scan(&ruleID, &enabled); err != nil || enabled {
			t.Fatal("rule did not start disabled")
		}
		if response := request("GET", "/rules?edit="+ruleID, token, "", "", ""); response.Code != 200 || !strings.Contains(response.Body.String(), "Preview cached matches") {
			t.Fatalf("rule UI unavailable: %s", response.Body.String())
		}
		if request("GET", "/rules?edit="+ruleID, other, "", "", "").Code != 404 {
			t.Fatal("foreign rule was readable")
		}
		if response := request("POST", "/rules/"+ruleID+"/preview", token, session.CSRF, "application/x-www-form-urlencoded", ""); response.Code != 200 || !strings.Contains(response.Body.String(), "Preview only") {
			t.Fatal("rule preview unavailable")
		}
		state := url.Values{"revision": {"1"}, "enabled": {"1"}}.Encode()
		if request("POST", "/rules/"+ruleID+"/enable", token, session.CSRF, "application/x-www-form-urlencoded", state).Code != 400 {
			t.Fatal("rule enabled without approval")
		}
		state = url.Values{"revision": {"1"}, "enabled": {"1"}, "consent": {"allow"}}.Encode()
		if request("POST", "/rules/"+ruleID+"/enable", other, otherSession.CSRF, "application/x-www-form-urlencoded", state).Code != 404 {
			t.Fatal("foreign rule enabled")
		}
		if request("POST", "/rules/"+ruleID+"/enable", token, session.CSRF, "application/x-www-form-urlencoded", state).Code != 303 {
			t.Fatal("approved rule not enabled")
		}
		if provider.calls != 1 {
			t.Fatal("local rule actions invoked an AI provider")
		}
	})

}
