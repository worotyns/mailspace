// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"html"
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
	netmail "net/mail"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	if ordinaryPage.Code != 200 || !strings.Contains(ordinaryPage.Body.String(), `data-ordinary="true"`) || !strings.Contains(ordinaryPage.Header().Get("Content-Security-Policy"), "frame-src 'self'") || !strings.Contains(ordinaryPage.Header().Get("Content-Security-Policy"), "img-src blob:") || !strings.Contains(ordinaryPage.Body.String(), "Preview HTML") {
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
	label, err := groups.Create(ctx, alice.ID, "label", "Todo", "", "blue")
	if err != nil {
		t.Fatal(err)
	}
	assignLabel := func(cookie, csrf string) *httptest.ResponseRecorder {
		values := url.Values{"csrf": {csrf}, "kind": {"label"}, "reference": {webui.EncodeSource(ref)}, "selected": {"1"}, "return_to": {"/?account=" + account.ID}}
		req := httptest.NewRequest("POST", "/collections/"+label.ID+"/membership", strings.NewReader(values.Encode()))
		req.Header.Set("Origin", "https://mail.example.test")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Target", "labels-message-"+sourceIdentity.Key)
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	if res := assignLabel(other, otherSession.CSRF); res.Code != 404 {
		t.Fatal("foreign header-label mutation accepted", res.Code)
	}
	if res := assignLabel(token, "wrong"); res.Code != 403 {
		t.Fatal("header-label mutation accepted without CSRF", res.Code)
	}
	if res := assignLabel(token, session.CSRF); res.Code != 200 || !strings.Contains(res.Body.String(), `id="labels-message-`+sourceIdentity.Key+`"`) || !strings.Contains(res.Body.String(), `hx-swap-oob="outerHTML"`) || strings.Contains(res.Body.String(), "data-local-preview") {
		t.Fatal("header-label response replaced private message content or omitted sidebar", res.Code, res.Body.String())
	}
	threadURL := "/?thread=" + sourceIdentity.Key + "&space=" + space.ID
	replyURL := "/?" + url.Values{"thread": {sourceIdentity.Key}, "space": {space.ID}, "reply": {sourceIdentity.Key}}.Encode() + "#thread-composer"
	if response = request("GET", threadURL, token, "", false); response.Code != 200 || !strings.Contains(response.Body.String(), "Original email thread") || !strings.Contains(response.Body.String(), `href="`+html.EscapeString(replyURL)+`"`) {
		t.Fatal("original thread unavailable", response.Code, response.Body.String())
	}
	if request("GET", "/?thread="+sourceIdentity.Key, other, "", false).Code == 200 {
		t.Fatal("foreign original thread disclosed")
	}
	if response = request("GET", threadURL+"&reply="+sourceIdentity.Key, token, "", false); response.Code != 200 || !strings.Contains(response.Body.String(), "thread-reply-form") {
		t.Fatal("thread reply composer unavailable")
	}

	t.Run("drawer and archive boundaries", func(t *testing.T) {
		req := httptest.NewRequest("GET", threadURL+"&reply="+sourceIdentity.Key, nil)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Target", "thread-content")
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: token})
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != 200 || !strings.Contains(res.Body.String(), `id="thread-feed"`) || !strings.Contains(res.Body.String(), `id="thread-reply-form"`) || strings.Contains(res.Body.String(), `id="timeline-shell"`) {
			t.Fatal("drawer did not return scoped fragment", res.Code)
		}
		archive := func(cookie, csrf, state string) *httptest.ResponseRecorder {
			req := httptest.NewRequest("POST", "/messages/archive", strings.NewReader(url.Values{"csrf": {csrf}, "reference": {webui.EncodeSource(ref)}, "archived": {state}}.Encode()))
			req.Header.Set("Origin", "https://mail.example.test")
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			return res
		}
		if archive(token, "", "1").Code != 403 {
			t.Fatal("archive CSRF bypass")
		}
		if archive(other, otherSession.CSRF, "1").Code != 404 {
			t.Fatal("foreign archive accepted")
		}
		if archive(token, session.CSRF, "\n 1\n").Code != 303 {
			t.Fatal("archive failed")
		}
		archived := request("GET", "/?filter=archived", token, "", false)
		if archived.Code != 200 || !strings.Contains(archived.Body.String(), `id="message-`+sourceIdentity.Key+`"`) {
			t.Fatal("archive view missing source")
		}
		if strings.Contains(request("GET", "/?space="+space.ID, token, "", false).Body.String(), `id="message-`+sourceIdentity.Key+`"`) {
			t.Fatal("archived source remains active")
		}
		if _, err := sourceService.Resolve(ctx, alice.ID, ref); err != nil {
			t.Fatal("archive deleted source")
		}
		if archive(token, session.CSRF, "0").Code != 303 {
			t.Fatal("restore failed")
		}
	})
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
	history := func(cookie, csrf, consent, mode string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/accounts/"+account.ID+"/history", strings.NewReader(url.Values{"csrf": {csrf}, "consent": {consent}, "mode": {mode}}.Encode()))
		req.Header.Set("Origin", "https://mail.example.test")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if history(token, "", "allow", "fresh").Code != 403 {
		t.Fatal("import policy bypassed CSRF")
	}
	if history(token, session.CSRF, "", "recent").Code != 400 {
		t.Fatal("history imported without approval")
	}
	if history(other, otherSession.CSRF, "allow", "recent").Code != 404 {
		t.Fatal("foreign history policy accepted")
	}
	if history(token, session.CSRF, "allow", "fresh").Code != 303 {
		t.Fatal("fresh policy unavailable")
	}
	if p, err := syncer.ImportPolicy(ctx, alice.ID, account.ID); err != nil || p.Mode != "fresh" {
		t.Fatal("fresh policy not saved")
	}
	if res := request("GET", "/accounts/"+account.ID+"/edit", token, "", false); res.Code != 200 || !strings.Contains(res.Body.String(), "Import recent history") {
		t.Fatal("history controls missing")
	}
	if history(token, session.CSRF, "allow", "recent").Code != 303 {
		t.Fatal("explicit recent import unavailable")
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
			secureURL := "/secure/compose?" + url.Values{"account": {account.ID}, "folder": {"Sent"}, "uid": {strconv.FormatUint(uint64(sentMessage.UID), 10)}, "validity": {strconv.FormatUint(uint64(sentMessage.Validity), 10)}}.Encode()
			if !strings.Contains(reply.Body.String(), `href="`+html.EscapeString(secureURL)+`"`) {
				t.Fatal("switching reply protection lost the selected original")
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
	encryptedInput := map[string]string{"account": account.ID, "requestId": "SYNTHETIC_HTTP_ENCRYPTED_SEND_12345", "to": "recipient@example.test, second@example.test", "cc": "copy@example.test", "bcc": "hidden@example.test", "subject": "Encrypted SMTP fixture", "encryptedMime": string(fixture)}
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

		encryptedHeaders, e := netmail.ReadMessage(bytes.NewReader(source.Body.Bytes()))
		if e != nil || !strings.Contains(encryptedHeaders.Header.Get("Cc"), "copy@example.test") || !strings.Contains(encryptedHeaders.Header.Get("To"), "second@example.test") || bytes.Contains(source.Body.Bytes(), []byte("hidden@example.test")) {
			t.Fatal("encrypted recipient headers or Bcc privacy changed")
		}
		for _, path := range []string{"/api/mail/raw?", "/secure/read?"} {
			if request("GET", path+q, other, "", false).Code != 404 {
				t.Fatal("foreign ciphertext read")
			}
		}
		if res := request("GET", "/secure/read?"+q, token, "", false); res.Code != 200 || strings.Contains(res.Body.String(), "<form") {
			t.Fatal("secure reader unavailable")
		}
		compose := request("GET", "/secure/compose?"+q, token, "", false)
		if compose.Code != 200 || !strings.Contains(compose.Body.String(), `data-encryption-required="true"`) || !regexp.MustCompile(`(?s)<input\s+id="secure-sign-only"[^>]*\bdisabled\b`).MatchString(compose.Body.String()) {
			t.Fatal("encrypted original permits choosing signing only")
		}
		invalid := strings.Replace(q, "uid="+strconv.FormatUint(uint64(m.UID), 10), "uid=4294967295", 1)
		if request("GET", "/api/mail/raw?"+invalid, token, "", false).Code != 404 {
			t.Fatal("stale identity accepted")
		}
	}

	t.Run("authored signed transport", func(t *testing.T) {
		// A fresh application limiter isolates this boundary's request budget
		// from the preceding ordinary/encrypted transport acceptance scenarios.
		handler, err := NewPersonal(dir, db, AuthConfig{Outbox: outgoing, Origin: "https://mail.example.test", Accounts: a, Sync: syncer, MailEndpointsEnabled: true})
		if err != nil {
			t.Fatal(err)
		}
		fixture, err := os.ReadFile("../outbox/testdata/signed.eml")
		if err != nil {
			t.Fatal(err)
		}
		input := map[string]any{"account": account.ID, "requestId": "SYNTHETIC_HTTP_SIGNED_SEND_12345", "to": "recipient@example.test", "bcc": "hidden@example.test", "subject": "Signed SMTP fixture", "signedMime": string(fixture), "provenance": "authored", "allowPlaintext": true}
		send := func(cookie, csrf string, payload map[string]any) *httptest.ResponseRecorder {
			encoded, _ := json.Marshal(payload)
			req := httptest.NewRequest("POST", "/api/outbox/signed", bytes.NewReader(encoded))
			req.Header.Set("Origin", "https://mail.example.test")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", csrf)
			req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			return res
		}
		if send(token, "", input).Code != 403 {
			t.Fatal("signed CSRF bypass")
		}
		if send(other, otherSession.CSRF, input).Code == 200 {
			t.Fatal("foreign signed account accepted")
		}
		for _, provenance := range []string{"", "client-decrypted", "unknown"} {
			input["provenance"] = provenance
			if send(token, session.CSRF, input).Code != 400 {
				t.Fatal("forbidden signed provenance accepted")
			}
		}
		input["provenance"] = "authored"
		input["allowPlaintext"] = false
		if send(token, session.CSRF, input).Code != 400 {
			t.Fatal("implicit signed plaintext consent")
		}
		input["allowPlaintext"] = true
		for _, field := range []string{"body", "encryptedMime", "privateKey", "passphrase", "decryptedMime", "headers"} {
			input[field] = "FORBIDDEN_SIGNED_FIELD"
			if send(token, session.CSRF, input).Code != 400 {
				t.Fatal("unknown signed field accepted", field)
			}
			delete(input, field)
		}
		stale := typedRef
		stale.Revision++
		input["reference"] = webui.EncodeSource(stale)
		if send(token, session.CSRF, input).Code == 200 {
			t.Fatal("stale signed reply accepted")
		}
		for _, message := range allSent {
			if message.Preview.Encrypted {
				input["reference"] = webui.EncodeSource(mail.SourceRef{Kind: "imap", AccountID: account.ID, Revision: account.Revision, Mailbox: "Sent", Validity: message.Validity, UID: message.UID})
				if send(token, session.CSRF, input).Code == 200 {
					t.Fatal("encrypted original downgraded by signed HTTP")
				}
			}
		}
		delete(input, "reference")
		var signedID string
		for i := 0; i < 2; i++ {
			response := send(token, session.CSRF, input)
			if response.Code != 200 {
				t.Fatal("signed SMTP failed", response.Code, response.Body.String())
			}
			var result map[string]string
			if json.Unmarshal(response.Body.Bytes(), &result) != nil || result["state"] != "accepted" || result["copyState"] != "saved" {
				t.Fatal("signed outcome incorrect", response.Body.String())
			}
			if signedID != "" && signedID != result["id"] {
				t.Fatal("signed replay duplicated outbox")
			}
			signedID = result["id"]
		}
		input["reference"] = webui.EncodeSource(typedRef)
		input["requestId"] = "SYNTHETIC_HTTP_SIGNED_REPLY_12345"
		if response := send(token, session.CSRF, input); response.Code != 200 {
			t.Fatal("current original signed reply refused", response.Code, response.Body.String())
		}
		if err = syncer.Sync(ctx, alice.ID, account.ID); err != nil {
			t.Fatal(err)
		}
		sent, err := syncer.Messages(ctx, alice.ID, account.ID, "Sent")
		if err != nil || len(sent) != 6 {
			t.Fatal("signed Sent missing or replay duplicated", len(sent), err)
		}
		count := 0
		for _, message := range sent {
			if message.Preview.Subject == "Signed SMTP fixture" {
				count++
				raw, err := syncer.Raw(ctx, alice.ID, account.ID, "Sent", message.Validity, message.UID)
				if err != nil || !bytes.HasSuffix(raw, fixture) || bytes.Contains(raw, []byte("hidden@example.test")) {
					t.Fatal("SMTP/Sent signed octets or Bcc boundary changed", err)
				}
				if _, err = syncer.Raw(ctx, bob.ID, account.ID, "Sent", message.Validity, message.UID); err == nil {
					t.Fatal("foreign signed Sent disclosed")
				}
				verifyTransportSignature(t, fixture)
			}
		}
		if count != 2 {
			t.Fatal("signed messages not found", count)
		}
	})
	t.Run("inline multi-recipient native send", func(t *testing.T) {
		handler, err := NewPersonal(dir, db, AuthConfig{Outbox: outgoing, Origin: "https://mail.example.test", Accounts: a, Sync: syncer, MailEndpointsEnabled: true})
		if err != nil {
			t.Fatal(err)
		}
		send := func(cookie, csrf string, values url.Values) *httptest.ResponseRecorder {
			values.Set("csrf", csrf)
			req := httptest.NewRequest("POST", "/timeline/replies/plain", strings.NewReader(values.Encode()))
			req.Header.Set("Origin", "https://mail.example.test")
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			return response
		}
		values := url.Values{"account": {account.ID}, "request_id": {"INLINE_RECIPIENT_GROUP_SEND_123456"}, "reference": {webui.EncodeSource(typedRef)}, "to": {"One <one@example.test>, Two <two@example.test>"}, "cc": {"Copy <copy@example.test>"}, "bcc": {"blind@example.test"}, "subject": {"Ignored reply subject"}, "body": {"Synthetic inline multi-recipient reply"}, "allow_plaintext": {"1"}, "return_to": {"/?account=" + account.ID}}
		if send(token, "wrong", values).Code != 403 {
			t.Fatal("inline recipient send bypassed CSRF")
		}
		if send(other, otherSession.CSRF, values).Code != 404 {
			t.Fatal("foreign inline recipient send accepted")
		}
		for i := 0; i < 2; i++ {
			if r := send(token, session.CSRF, values); r.Code != 303 {
				t.Fatal("native inline multi-recipient reply failed", r.Code)
			}
		}
		var raw []byte
		var encoded, state, copyState string
		if err := db.Database().QueryRowContext(ctx, "SELECT raw,recipients,state,copy_state FROM outbox WHERE account_id=? AND nonce=?", account.ID, values.Get("request_id")).Scan(&raw, &encoded, &state, &copyState); err != nil {
			t.Fatal(err)
		}
		var recipients []string
		if err := json.Unmarshal([]byte(encoded), &recipients); err != nil || len(recipients) != 4 || state != "accepted" || copyState != "saved" {
			t.Fatal("inline envelope/SMTP/Sent result incorrect")
		}
		for _, address := range []string{"one@example.test", "two@example.test", "copy@example.test", "blind@example.test"} {
			found := false
			for _, actual := range recipients {
				if actual == address {
					found = true
				}
			}
			if !found {
				t.Fatal("inline envelope lost an explicitly entered recipient")
			}
		}
		message, err := netmail.ReadMessage(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		tos, err := message.Header.AddressList("To")
		if err != nil || len(tos) != 2 {
			t.Fatal("multiple To headers lost")
		}
		ccs, err := message.Header.AddressList("Cc")
		if err != nil || len(ccs) != 1 {
			t.Fatal("Cc header lost")
		}
		if message.Header.Get("Bcc") != "" || bytes.Contains(raw, []byte("blind@example.test")) || message.Header.Get("In-Reply-To") != "<"+messages[0].Preview.MessageID+">" {
			t.Fatal("Bcc privacy or original reply reference changed")
		}
		values.Set("request_id", "INLINE_BLIND_ONLY_SEND_1234567890")
		values.Del("to")
		values.Del("cc")
		values.Del("reference")
		values.Set("subject", "Bcc only")
		if r := send(token, session.CSRF, values); r.Code != 303 {
			t.Fatal("Bcc-only native send failed", r.Code)
		}
		if err := db.Database().QueryRowContext(ctx, "SELECT raw,recipients FROM outbox WHERE account_id=? AND nonce=?", account.ID, values.Get("request_id")).Scan(&raw, &encoded); err != nil {
			t.Fatal(err)
		}
		message, err = netmail.ReadMessage(bytes.NewReader(raw))
		if err != nil || message.Header.Get("To") != "undisclosed-recipients:;" || message.Header.Get("Bcc") != "" || bytes.Contains(raw, []byte("blind@example.test")) {
			t.Fatal("Bcc-only visible header boundary changed")
		}
		if r := request("GET", "/outbox?account="+account.ID, other, "", false); r.Code != 404 {
			t.Fatal("foreign outbox recipient metadata disclosed")
		}
	})

	t.Run("bounded upward cached history", func(t *testing.T) {
		tx, err := db.Database().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for i := 0; i < 55; i++ {
			raw := []byte(fmt.Sprintf("From: history@example.test\r\nTo: demo@example.test\r\nSubject: History %02d\r\nMessage-ID: <paging-%d@example.test>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nSynthetic cached history %d.", i, i, i))
			preview, _ := json.Marshal(mailsync.Parse(raw))
			if _, err := tx.ExecContext(ctx, "INSERT INTO mail_messages(account_id,mailbox,uid,flags,size,received_at,raw,preview) VALUES(?, 'INBOX',?, '[]',?,?,?,?)", account.ID, 10000+i, len(raw), time.Now().Add(24*time.Hour).Unix()+int64(i), raw, string(preview)); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		view := "/?" + url.Values{"account": {account.ID}, "filter": {"all"}}.Encode()
		latest := request("GET", view, token, "", false)
		keys := regexp.MustCompile(`data-message-key="([a-f0-9]+)"`)
		firstKeys := keys.FindAllStringSubmatch(latest.Body.String(), -1)
		if latest.Code != 200 || len(firstKeys) != 20 || !strings.Contains(latest.Body.String(), "History 54") || strings.Contains(latest.Body.String(), "History 34") {
			t.Fatal("initial HTTP view is not latest 20")
		}
		linkPattern := regexp.MustCompile(`(?s)<a\s+href="([^"]+)"[^>]*data-history-load`)
		match := linkPattern.FindStringSubmatch(latest.Body.String())
		if len(match) != 2 {
			t.Fatal("native history link missing")
		}
		older := html.UnescapeString(match[1])
		parsed, _ := url.Parse(older)
		historyURL := "/timeline/history?" + parsed.RawQuery
		load := func(cookie string, target string) *httptest.ResponseRecorder {
			req := httptest.NewRequest("GET", historyURL, nil)
			req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: cookie})
			req.Header.Set("HX-Request", "true")
			req.Header.Set("HX-Target", target)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			return response
		}
		page := load(token, "timeline-history")
		secondKeys := keys.FindAllStringSubmatch(page.Body.String(), -1)
		if page.Code != 200 || len(secondKeys) != 20 || strings.Contains(page.Body.String(), `id="timeline-shell"`) || strings.Contains(page.Body.String(), "data-reply-body") || !strings.Contains(page.Body.String(), "History 15") || strings.Contains(page.Body.String(), "History 35") {
			t.Fatal("history fragment scope/boundary wrong", page.Code)
		}
		seen := map[string]bool{}
		for _, m := range append(firstKeys, secondKeys...) {
			if seen[m[1]] {
				t.Fatal("HTTP cursor duplicated a message")
			}
			seen[m[1]] = true
		}
		native := request("GET", older, token, "", false)
		if native.Code != 200 || len(keys.FindAllStringSubmatch(native.Body.String(), -1)) != 20 || !strings.Contains(native.Body.String(), "Latest messages") {
			t.Fatal("native older page missing")
		}
		if load(other, "timeline-history").Code == 200 || load("", "timeline-history").Code != 401 || load(token, "timeline-shell").Code != 400 {
			t.Fatal("history ownership/target boundary failed")
		}
		if request("GET", historyURL, token, "", false).Code != 303 {
			t.Fatal("direct fragment navigation lacks native fallback")
		}
		// The list/scroll path must not mark cached mail read before that separate
		// provider capability is explicitly implemented and enabled.
		var flags string
		if err := db.Database().QueryRowContext(ctx, "SELECT flags FROM mail_messages WHERE account_id=? AND mailbox='INBOX' AND uid=10000", account.ID).Scan(&flags); err != nil || flags != "[]" {
			t.Fatal("history read mutated flags")
		}
	})

}

func verifyTransportSignature(t *testing.T, entity []byte) {
	t.Helper()
	if _, err := exec.LookPath("gpg"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("GnuPG is required in CI")
		}
		t.Skip("GnuPG unavailable: transport verification remains pending")
	}
	// The HTTP→SMTP→Sent test already proved byte identity with this entity.
	// Split its exact signed octets; do not decode or reconstruct MIME headers.
	_, body, ok := bytes.Cut(entity, []byte("\r\n\r\n"))
	if !ok {
		t.Fatal("fixture headers missing")
	}
	line, rest, ok := bytes.Cut(body, []byte("\r\n"))
	if !ok {
		t.Fatal("fixture delimiter missing")
	}
	data, signaturePart, ok := bytes.Cut(rest, append(append([]byte("\r\n"), line...), []byte("\r\n")...))
	if !ok {
		t.Fatal("signature part missing")
	}
	_, signature, ok := bytes.Cut(signaturePart, []byte("\r\n\r\n"))
	if !ok {
		t.Fatal("signature headers missing")
	}
	signature, _, ok = bytes.Cut(signature, append(append([]byte("\r\n"), line...), []byte("--\r\n")...))
	if !ok {
		t.Fatal("signature terminator missing")
	}
	// Unix-domain agent sockets need a short path (not the long subtest name).
	home, err := os.MkdirTemp("", "ms-gpg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("gpgconf", "--homedir", home, "--kill", "all").Run()
		_ = os.RemoveAll(home)
	})
	pub, err := os.ReadFile("../outbox/testdata/signed-public.asc")
	if err != nil {
		t.Fatal(err)
	}
	keyPath, dataPath, sigPath := filepath.Join(home, "public.asc"), filepath.Join(home, "entity"), filepath.Join(home, "signature.asc")
	for path, content := range map[string][]byte{keyPath: pub, dataPath: data, sigPath: signature} {
		if err = os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.Command("gpg", "--homedir", home, "--batch", "--import", keyPath).CombinedOutput(); err != nil {
		t.Fatalf("synthetic public key import failed: %v %s", err, output)
	}
	if output, err := exec.Command("gpg", "--homedir", home, "--batch", "--verify", sigPath, dataPath).CombinedOutput(); err != nil {
		t.Fatalf("transport signature failed: %v %s", err, output)
	}
	data = bytes.Clone(data)
	data[len(data)-4] ^= 1
	if err = os.WriteFile(dataPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if exec.Command("gpg", "--homedir", home, "--batch", "--verify", sigPath, dataPath).Run() == nil {
		t.Fatal("GnuPG accepted mutated transport bytes")
	}
}
