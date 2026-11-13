// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/devmail"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailnet"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"mailspace.local/mailspace/internal/timeline"
	"mailspace.local/mailspace/internal/webui"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyMailboxEndToEndAndAuthorization(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	server, err := devmail.Start(devmail.Options{StateDir: filepath.Join(dir, "provider"), IMAPAddr: "127.0.0.1:0", SMTPAddr: "127.0.0.1:0", Username: "demo@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	roots := x509.NewCertPool()
	pem, err := os.ReadFile(server.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	roots.AppendCertsFromPEM(pem)
	password, err := os.ReadFile(server.PasswordFile)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := mailnet.NewPolicy("", server.IMAPAddr+","+server.SMTPAddr)
	if err != nil {
		t.Fatal(err)
	}
	connector := &mailnet.Connector{Policy: policy, Roots: roots, Timeout: 3 * time.Second}
	endpoint := func(addr string) mailnet.Endpoint {
		host, port, _ := net.SplitHostPort(addr)
		p, _ := strconv.Atoi(port)
		return mailnet.Endpoint{Host: host, Port: p, TLS: "tls"}
	}
	db, err := identity.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	alice, err := db.CreateUser(ctx, "alice@example.test", "synthetic app password")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := db.CreateUser(ctx, "bob@example.test", "another synthetic app password")
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
	a, err := accounts.Open(db.Database(), filepath.Join(dir, "keys"), connector)
	if err != nil {
		t.Fatal(err)
	}
	login := mailnet.Login{Username: server.Username, Password: string(password)}
	account, err := a.Save(ctx, alice.ID, "", accounts.Input{Name: "Actual synthetic mailbox", Address: server.Username, IMAP: endpoint(server.IMAPAddr), SMTP: endpoint(server.SMTPAddr), IMAPLogin: login, SMTPLogin: login})
	if err != nil {
		t.Fatal(err)
	}
	syncer := mailsync.New(db.Database(), a, connector)
	outgoing := outbox.New(db.Database(), a, syncer, connector)
	handler, err := NewPersonal(dir, db, AuthConfig{Outbox: outgoing, Origin: "https://mail.example.test", Accounts: a, Sync: syncer, MailEndpointsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, cookie, csrf string, hx bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(url.Values{"csrf": {csrf}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://mail.example.test")
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		if hx {
			req.Header.Set("HX-Request", "true")
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	path := "/accounts/" + account.ID + "/sync"
	if res := request("POST", path, token, "", false); res.Code != 403 {
		t.Fatal("sync bypassed CSRF")
	}
	if res := request("POST", path, other, otherSession.CSRF, false); res.Code != 404 {
		t.Fatal("foreign owner synchronized")
	}
	before, err := connector.OpenRead(ctx, endpoint(server.IMAPAddr), login)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := before.Snapshot("INBOX")
	before.Close()
	if err != nil {
		t.Fatal(err)
	}
	response := request("POST", path, token, session.CSRF, false)
	if response.Code != 303 || !strings.Contains(response.Header().Get("Location"), "synced") {
		t.Fatal("full TLS sync failed", response.Code, response.Body.String())
	}
	messages, err := syncer.Messages(ctx, alice.ID, account.ID, "INBOX")
	if err != nil || len(messages) != 13 {
		t.Fatal("IMAP seed not persisted", len(messages), err)
	}
	ordinaryQuery := url.Values{"account": {account.ID}, "folder": {"INBOX"}, "uid": {strconv.FormatUint(uint64(messages[0].UID), 10)}, "validity": {strconv.FormatUint(uint64(messages[0].Validity), 10)}}.Encode()
	ordinaryPage := request("GET", "/secure/read?"+ordinaryQuery, token, "", false)
	if ordinaryPage.Code != 200 || !strings.Contains(ordinaryPage.Body.String(), `data-ordinary="true"`) || !strings.Contains(ordinaryPage.Header().Get("Content-Security-Policy"), "frame-src 'self'") || !strings.Contains(ordinaryPage.Header().Get("Content-Security-Policy"), "img-src 'none'") || !strings.Contains(ordinaryPage.Body.String(), "Preview HTML") {
		t.Fatal("ordinary text reader/preview boundary missing", ordinaryPage.Code)
	}
	if request("GET", "/secure/read?"+ordinaryQuery, other, "", false).Code != 404 {
		t.Fatal("foreign ordinary reader disclosed raw source")
	}
	folders, err := syncer.Folders(ctx, alice.ID, account.ID)
	if err != nil || len(folders) != 3 {
		t.Fatal("real folders missing", folders, err)
	}
	for _, m := range messages {
		if m.Preview.Status != "ready" {
			t.Fatal("MIME preview failed", m.Preview.Status)
		}
	}
	after, err := connector.OpenRead(ctx, endpoint(server.IMAPAddr), login)
	if err != nil {
		t.Fatal(err)
	}
	newSnapshot, err := after.Snapshot("INBOX")
	after.Close()
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range snapshot.Messages {
		if strings.Join(m.Flags, ",") != strings.Join(newSnapshot.Messages[i].Flags, ",") {
			t.Fatal("BODY.PEEK/EXAMINE changed flags")
		}
	}
	mailbox := "/mail?account=" + url.QueryEscape(account.ID)
	response = request("GET", mailbox, token, "", false)
	if response.Code != 303 || response.Header().Get("Location") != "/?account="+account.ID {
		t.Fatal("legacy mailbox was not redirected", response.Code)
	}
	if request("GET", mailbox, other, "", false).Code != 404 {
		t.Fatal("foreign legacy account access")
	}
	response = request("GET", mailbox+"&view=classic", token, "", true)
	if response.Code != 303 || response.Header().Get("HX-Redirect") == "" || strings.Contains(response.Body.String(), "SUBJECT &amp; PREVIEW") {
		t.Fatal("retired classic view survived")
	}
	if response = request("GET", "/", token, "", false); response.Code != 200 || !strings.Contains(response.Body.String(), "timeline-feed") {
		t.Fatal("root is not timeline", response.Code)
	}
	if response = request("GET", "/settings", token, "", false); response.Code != 200 {
		t.Fatal("settings unavailable")
	}

	if response = request("GET", "/", token, "", false); response.Code != 200 || !strings.Contains(response.Body.String(), "timeline-feed") || strings.Contains(response.Body.String(), "view=calendar") {
		t.Fatal("unified timeline unavailable", response.Code, response.Body.String())
	}
	if response = request("GET", "/?account="+account.ID, other, "", false); response.Code == 200 {
		t.Fatal("foreign timeline disclosed account")
	}
	if response = request("GET", "/", "", "", true); response.Code != 401 || response.Header().Get("HX-Redirect") != "/login" {
		t.Fatal("expired session injected a login fragment")
	}
	sourceService := timeline.New(db, a, syncer, outgoing)
	groups := collections.New(db.Database(), sourceService)
	space, err := groups.Create(ctx, alice.ID, "space", "Work", "Synthetic classification brief", "blue")
	if err != nil {
		t.Fatal(err)
	}
	ref := mail.SourceRef{Kind: "imap", AccountID: account.ID, Revision: account.Revision, Mailbox: "INBOX", UID: messages[0].UID, Validity: messages[0].Validity}
	assign := func(cookie, csrf, selected string) *httptest.ResponseRecorder {
		values := url.Values{"csrf": {csrf}, "kind": {"space"}, "reference": {webui.EncodeSource(ref)}, "selected": {selected}}
		req := httptest.NewRequest("POST", "/collections/"+space.ID+"/membership", strings.NewReader(values.Encode()))
		req.Header.Set("Origin", "https://mail.example.test")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	collectionURL := "/messages/collections?" + url.Values{"reference": {webui.EncodeSource(ref)}}.Encode()
	if res := request("GET", collectionURL, token, "", false); res.Code != 200 || !strings.Contains(res.Body.String(), "Organize this message") {
		t.Fatal("assignment fallback failed")
	}
	if request("GET", collectionURL, other, "", true).Code != 404 {
		t.Fatal("foreign assignment choices disclosed")
	}
	if assign(other, otherSession.CSRF, "1").Code != 404 {
		t.Fatal("foreign membership accepted")
	}
	if assign(token, session.CSRF, " 1 ").Code != 400 {
		t.Fatal("ambiguous selection accepted")
	}
	if res := assign(token, session.CSRF, "1"); res.Code != 200 || !strings.Contains(res.Body.String(), "Work") {
		t.Fatal("manual assignment failed", res.Code, res.Body.String())
	}
	if res := request("GET", "/?space="+space.ID, token, "", false); res.Code != 200 || !strings.Contains(res.Body.String(), messages[0].Preview.Subject) {
		t.Fatal("assigned message absent")
	}
	if request("GET", "/?space="+space.ID, other, "", false).Code != 404 {
		t.Fatal("foreign Space read")
	}
	if response = request("GET", "/timeline?filter=unsorted", token, "", false); response.Code != 303 || response.Header().Get("Location") != "/?filter=unsorted" {
		t.Fatal("old timeline alias lost context")
	}
	sourceIdentity, err := sourceService.Identity(ctx, alice.ID, ref)
	if err != nil {
		t.Fatal(err)
	}
	threadURL := "/?thread=" + sourceIdentity.Key + "&space=" + space.ID
	if response = request("GET", threadURL, token, "", false); response.Code != 200 || !strings.Contains(response.Body.String(), "Original email thread") || !strings.Contains(response.Body.String(), "View email thread") {
		t.Fatal("original thread unavailable", response.Code, response.Body.String())
	}
	if request("GET", "/?thread="+sourceIdentity.Key, other, "", false).Code == 200 {
		t.Fatal("foreign original thread disclosed")
	}
	if response = request("GET", threadURL+"&reply="+sourceIdentity.Key, token, "", false); response.Code != 200 || !strings.Contains(response.Body.String(), "inline-reply-form") {
		t.Fatal("thread reply composer unavailable")
	}
	removeSpace := func(cookie, csrf, confirmation string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/spaces/"+space.ID+"/delete", strings.NewReader(url.Values{"csrf": {csrf}, "confirm": {confirmation}}.Encode()))
		req.Header.Set("Origin", "https://mail.example.test")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	if removeSpace(other, otherSession.CSRF, "remove").Code != 404 {
		t.Fatal("foreign Space removal accepted")
	}
	if removeSpace(token, session.CSRF, "").Code != 400 {
		t.Fatal("Space removed without confirmation")
	}
	if removeSpace(token, session.CSRF, "remove").Code != 303 {
		t.Fatal("confirmed removal failed")
	}
	if _, err = sourceService.Resolve(ctx, alice.ID, ref); err != nil {
		t.Fatal("removing Space deleted original mail")
	}
	compose := request("GET", "/compose?account="+account.ID, token, "", false)
	if compose.Code != 200 || !strings.Contains(compose.Body.String(), "Send this message without OpenPGP encryption") {
		t.Fatal("compose missing explicit encryption decision")
	}
	form := url.Values{"csrf": {session.CSRF}, "account": {account.ID}, "request_id": {"SYNTHETIC_HTTP_SEND_REQUEST_12345"}, "to": {"recipient@example.test"}, "bcc": {"hidden@example.test"}, "subject": {"End-to-end SMTP"}, "body": {"Only the isolated SMTP sink receives this message."}, "allow_plaintext": {"1"}}
	send := func(cookie string, values url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/outbox/send", strings.NewReader(values.Encode()))
		req.Header.Set("Origin", "https://mail.example.test")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	denied := send(other, url.Values{"csrf": {otherSession.CSRF}, "account": {account.ID}, "request_id": {"SYNTHETIC_HTTP_SEND_REQUEST_12345"}, "allow_plaintext": {"1"}})
	if denied.Code != 404 {
		t.Fatal("foreign user could queue mail")
	}
	sent := send(token, form)
	if sent.Code != 303 {
		t.Fatal("SMTP UI send failed", sent.Code, sent.Body.String())
	}
	if sent = send(token, form); sent.Code != 303 {
		t.Fatal("repeat send form did not resolve to original result")
	}
	records, err := outgoing.List(ctx, alice.ID, account.ID)
	if err != nil || len(records) != 1 || records[0].State != "accepted" || records[0].CopyState != "saved" {
		t.Fatal("real SMTP/APPEND result wrong", records, err)
	}
	if err = syncer.Sync(ctx, alice.ID, account.ID); err != nil {
		t.Fatal(err)
	}
	sentMessages, err := syncer.Messages(ctx, alice.ID, account.ID, "Sent")
	if err != nil || len(sentMessages) != 3 {
		t.Fatal("Sent append missing or duplicated", len(sentMessages), err)
	}

	for _, sentMessage := range sentMessages {
		if sentMessage.Preview.Subject == "End-to-end SMTP" {
			reply := request("GET", "/compose?account="+account.ID+"&folder=Sent&uid="+strconv.FormatUint(uint64(sentMessage.UID), 10)+"&validity="+strconv.FormatUint(uint64(sentMessage.Validity), 10), token, "", false)
			if reply.Code != 200 || !strings.Contains(reply.Body.String(), `value="recipient@example.test"`) {
				t.Fatal("reply to own sent message targeted self instead of original recipient", reply.Code)
			}
		}
	}
	response = request("GET", "/outbox?account="+account.ID, token, "", false)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Accepted by mail server") {
		t.Fatal("delivery result missing in UI")
	}
	// The encrypted route transports only structurally validated opaque MIME.
	securePage := request("GET", "/secure/compose?account="+account.ID, token, "", false)
	if securePage.Code != 200 || strings.Contains(securePage.Body.String(), "<form") || strings.Contains(securePage.Body.String(), `name="body"`) || !strings.Contains(securePage.Body.String(), "hx-disable") || !strings.Contains(securePage.Header().Get("Content-Security-Policy"), "form-action 'none'") {
		t.Fatal("secure controls can serialize private plaintext")
	}
	if request("GET", "/secure/compose?account="+account.ID, other, "", false).Code != 404 {
		t.Fatal("foreign encrypted composer disclosed account")
	}
	fixture, e := os.ReadFile("../outbox/testdata/encrypted.eml")
	if e != nil {
		t.Fatal(e)
	}
	encryptedInput := map[string]string{"account": account.ID, "requestId": "SYNTHETIC_HTTP_ENCRYPTED_SEND_12345", "to": "recipient@example.test", "bcc": "hidden@example.test", "subject": "Encrypted SMTP fixture", "encryptedMime": string(fixture)}
	sendEncrypted := func(cookie, csrf string, payload map[string]string) *httptest.ResponseRecorder {
		encoded, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/api/outbox/encrypted", bytes.NewReader(encoded))
		req.Header.Set("Origin", "https://mail.example.test")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	if sendEncrypted(token, "", encryptedInput).Code != 403 {
		t.Fatal("encrypted submission bypassed CSRF")
	}
	if sendEncrypted(other, otherSession.CSRF, encryptedInput).Code == 200 {
		t.Fatal("foreign encrypted submission accepted")
	}
	typedRef := mail.SourceRef{Kind: "imap", AccountID: account.ID, Revision: account.Revision, Mailbox: "INBOX", Validity: messages[0].Validity, UID: messages[0].UID}
	encryptedInput["reference"] = webui.EncodeSource(typedRef)
	if sendEncrypted(other, otherSession.CSRF, encryptedInput).Code == 200 {
		t.Fatal("foreign typed reply accepted")
	}
	stale := typedRef
	stale.Revision++
	encryptedInput["reference"] = webui.EncodeSource(stale)
	if sendEncrypted(token, session.CSRF, encryptedInput).Code == 200 {
		t.Fatal("stale typed reply accepted")
	}
	encryptedInput["reference"] = webui.EncodeSource(typedRef)
	encryptedInput["body"] = "FORBIDDEN_PLAINTEXT_CANARY"
	if sendEncrypted(token, session.CSRF, encryptedInput).Code != 400 {
		t.Fatal("generic plaintext field accepted")
	}
	delete(encryptedInput, "body")
	if res := sendEncrypted(token, session.CSRF, encryptedInput); res.Code != 200 {
		t.Fatal("encrypted SMTP failed", res.Code, res.Body.String())
	}
	if res := sendEncrypted(token, session.CSRF, encryptedInput); res.Code != 200 {
		t.Fatal("ciphertext replay failed")
	}
	if err = syncer.Sync(ctx, alice.ID, account.ID); err != nil {
		t.Fatal(err)
	}
	allSent, err := syncer.Messages(ctx, alice.ID, account.ID, "Sent")
	if err != nil || len(allSent) != 4 {
		t.Fatal("encrypted Sent copy missing or duplicated", len(allSent), err)
	}
	for _, m := range allSent {
		if m.Preview.Subject != "Encrypted SMTP fixture" {
			continue
		}
		q := url.Values{"account": {account.ID}, "folder": {"Sent"}, "validity": {strconv.FormatUint(uint64(m.Validity), 10)}, "uid": {strconv.FormatUint(uint64(m.UID), 10)}}.Encode()
		source := request("GET", "/api/mail/raw?"+q, token, "", false)
		if source.Code != 200 || source.Header().Get("Content-Type") != "application/octet-stream" || !mailsync.Parse(source.Body.Bytes()).Encrypted || bytes.Contains(source.Body.Bytes(), []byte("Synthetic encrypted fixture")) {
			t.Fatal("raw ciphertext boundary failed")
		}
		for _, path := range []string{"/api/mail/raw?", "/secure/read?"} {
			if request("GET", path+q, other, "", false).Code != 404 {
				t.Fatal("foreign ciphertext read")
			}
		}
		if res := request("GET", "/secure/read?"+q, token, "", false); res.Code != 200 || strings.Contains(res.Body.String(), "<form") {
			t.Fatal("secure reader unavailable")
		}
		invalid := strings.Replace(q, "uid="+strconv.FormatUint(uint64(m.UID), 10), "uid=4294967295", 1)
		if request("GET", "/api/mail/raw?"+invalid, token, "", false).Code != 404 {
			t.Fatal("stale identity accepted")
		}
	}

}
