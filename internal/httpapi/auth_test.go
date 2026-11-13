// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"mailspace.local/mailspace/internal/identity"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersonalSessionsAndAuthorization(t *testing.T) {
	store, err := identity.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	alice, err := store.CreateUser(context.Background(), "alice@example.test", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.CreateUser(context.Background(), "bob@example.test", "a completely different password")
	if err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateAccount(context.Background(), alice.ID, "Alice private")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPersonal(t.TempDir(), store, AuthConfig{Origin: "https://mail.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, origin string, body url.Values, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		var reader io.Reader
		if body != nil {
			reader = strings.NewReader(body.Encode())
		}
		req := httptest.NewRequest(method, "https://mail.example.test"+path, reader)
		if body != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	login := func(email, password string) *http.Cookie {
		t.Helper()
		response := request("POST", "/auth/login", "https://mail.example.test", url.Values{"email": {email}, "password": {password}}, nil, "")
		if response.Code != 303 {
			t.Fatalf("login failed %d %s", response.Code, response.Body.String())
		}
		cookies := response.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("session cookie missing")
		}
		return cookies[0]
	}
	if response := request("GET", "/api/accounts", "", nil, nil, ""); response.Code != 401 {
		t.Fatal("anonymous account access")
	}
	if response := request("POST", "/auth/login", "https://evil.test", url.Values{"email": {alice.Email}, "password": {"correct horse battery staple"}}, nil, ""); response.Code != 403 {
		t.Fatal("login CSRF accepted")
	}
	if response := request("POST", "/auth/login", "", url.Values{"email": {alice.Email}, "password": {"correct horse battery staple"}}, nil, ""); response.Code != 403 {
		t.Fatal("missing Origin accepted")
	}
	bad := request("POST", "/auth/login", "https://mail.example.test", url.Values{"email": {alice.Email}, "password": {"wrong password"}}, nil, "")
	if bad.Code != 401 || !strings.Contains(bad.Header().Get("Content-Type"), "text/html") || !strings.Contains(bad.Body.String(), "Email or password not recognized.") {
		t.Fatal("incorrect login response")
	}
	if response := request("GET", "/login", "", nil, nil, ""); response.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatal("form Origin can be suppressed by referrer policy")
	}
	aliceCookie := login(alice.Email, "correct horse battery staple")
	if aliceCookie.Name != "__Host-mailspace" || !aliceCookie.HttpOnly || !aliceCookie.Secure || aliceCookie.Path != "/" || aliceCookie.Domain != "" || aliceCookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	bobCookie := login(bob.Email, "a completely different password")
	if response := request("GET", "/api/accounts/"+private.ID, "", nil, bobCookie, ""); response.Code != 404 {
		t.Fatal("cross-user account read")
	}
	if response := request("GET", "/api/accounts", "", nil, bobCookie, ""); response.Code != 200 || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatal("cross-user account list")
	}
	if response := request("GET", "/api/accounts/"+private.ID, "", nil, aliceCookie, ""); response.Code != 200 || !strings.Contains(response.Body.String(), private.Name) {
		t.Fatal("owner lost access")
	}
	var session struct {
		User identity.User
		CSRF string `json:"csrfToken"`
	}
	response := request("GET", "/api/session", "", nil, aliceCookie, "")
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.User.ID != alice.ID || session.CSRF == "" {
		t.Fatal("invalid session contract")
	}
	if response := request("POST", "/auth/logout", "https://mail.example.test", nil, aliceCookie, ""); response.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if response := request("POST", "/auth/logout", "https://evil.test", nil, aliceCookie, session.CSRF); response.Code != 403 {
		t.Fatal("cross-origin logout accepted")
	}
	if response := request("POST", "/auth/logout", "https://mail.example.test", nil, aliceCookie, "wrong"); response.Code != 403 {
		t.Fatal("invalid CSRF accepted")
	}
	replacement := login(alice.Email, "correct horse battery staple")
	if replacement.Value == aliceCookie.Value {
		t.Fatal("session token reused")
	}
	response = request("POST", "/auth/logout-all", "https://mail.example.test", nil, aliceCookie, session.CSRF)
	if response.Code != 303 {
		t.Fatalf("revoke-all failed: %d", response.Code)
	}
	if response := request("GET", "/api/session", "", nil, replacement, ""); response.Code != 401 {
		t.Fatal("revoke-all left another session active")
	}
	if response := request("GET", "/api/session", "", nil, bobCookie, ""); response.Code != 200 {
		t.Fatal("revoke-all affected another user")
	}
	rotated := request("POST", "/auth/login", "https://mail.example.test", url.Values{"email": {bob.Email}, "password": {"a completely different password"}}, bobCookie, "")
	if rotated.Code != 303 {
		t.Fatal("session rotation failed")
	}
	newBob := rotated.Result().Cookies()[0]
	if response := request("GET", "/api/session", "", nil, bobCookie, ""); response.Code != 401 {
		t.Fatal("old token survived login rotation")
	}
	response = request("GET", "/api/session", "", nil, newBob, "")
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	logout := request("POST", "/auth/logout", "https://mail.example.test", url.Values{"csrf": {session.CSRF}}, newBob, "")
	if logout.Code != 303 || len(logout.Result().Cookies()) != 1 || logout.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatal("logout did not expire cookie")
	}
	if response := request("GET", "/api/session", "", nil, newBob, ""); response.Code != 401 {
		t.Fatal("logout left active token")
	}
	// Use a fresh user session to check that the personal mailbox stays empty.
	bobCookie = login(bob.Email, "a completely different password")
	if response := request("GET", "/api/conversations", "", nil, bobCookie, ""); strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatal("personal mode leaked shared demo seed")
	}
}
func TestInsecureHTTPRestrictedToLoopback(t *testing.T) {
	for _, origin := range []string{"http://mail.example.test", "https://mail.test/path", "https://user:password@mail.test", "https://mail.test/?secret=x", "file:///tmp/mailspace"} {
		if _, err := NewPersonal("", nil, AuthConfig{Origin: origin, InsecureLoopback: true}); err == nil {
			t.Fatalf("unsafe origin accepted: %s", origin)
		}
	}
	if _, err := NewPersonal("", nil, AuthConfig{Origin: "http://127.0.0.1:8080"}); err == nil {
		t.Fatal("HTTP silently enabled")
	}
	if _, err := NewPersonal("", nil, AuthConfig{Origin: "http://127.0.0.1:8080", InsecureLoopback: true}); err != nil {
		t.Fatal("explicit loopback dev mode rejected", err)
	}
}
func TestLoginLimiter(t *testing.T) {
	limiter := &loginLimiter{entries: map[string]windowEntry{}}
	now := time.Now()
	for i := 0; i < 10; i++ {
		if !limiter.allow("one-ip", now) {
			t.Fatal("limit too early")
		}
	}
	if limiter.allow("one-ip", now) {
		t.Fatal("limit bypassed")
	}
	if !limiter.allow("other-ip", now) {
		t.Fatal("unrelated IP blocked")
	}
	if !limiter.allow("one-ip", now.Add(time.Minute)) {
		t.Fatal("window did not reset")
	}
	for i := 0; i < 4096; i++ {
		limiter.entries[string(rune(i))] = windowEntry{started: now}
	}
	if limiter.allow("new-ip", now) {
		t.Fatal("unbounded rate-limit state")
	}
}
