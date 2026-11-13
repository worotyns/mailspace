// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"mailspace.local/mailspace/internal/identity"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLocalKeyPageHasNoUploadOrFormEgress(t *testing.T) {
	s, err := identity.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user, err := s.CreateUser(context.Background(), "keys@example.test", "synthetic app password")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.NewSession(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewPersonal(t.TempDir(), s, AuthConfig{Origin: "https://mail.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if authenticated {
			req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: token})
		}
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	if response := request("GET", "/keys", false); response.Code != 303 || response.Header().Get("Location") != "/login" {
		t.Fatal("unauthenticated UI did not return to login")
	}

	for _, path := range []string{"/settings", "/keys", "/profile"} {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: token})
		req.Header.Set("Sec-Fetch-Dest", "iframe")
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != 200 || !strings.Contains(res.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
			t.Fatal("trusted panel unavailable", path, res.Code, res.Header())
		}
		if path == "/keys" && !strings.Contains(res.Header().Get("Content-Security-Policy"), "connect-src 'none'") {
			t.Fatal("framed keys can transmit")
		}
	}
	response := request("GET", "/keys", true)
	if response.Code != 200 {
		t.Fatal("key view unavailable", response.Code)
	}
	csp := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'none'") || !strings.Contains(csp, "form-action 'none'") || strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Fatal("key page can transmit or execute dynamic code", csp)
	}
	body := response.Body.String()
	if strings.Contains(body, "<form") || regexp.MustCompile(`(?s)<(?:input|textarea)[^>]*\bname=`).MatchString(body) || !strings.Contains(body, "hx-disable") || !strings.Contains(body, `data-user-id="`+user.ID+`"`) {
		t.Fatal("key controls could serialize into a server form")
	}
	if response = request("POST", "/keys", true); response.Code != 404 && response.Code != 405 {
		t.Fatal("key upload endpoint unexpectedly exists")
	}
	if response = request("POST", "/api/keys", true); response.Code != 404 {
		t.Fatal("private key API unexpectedly exists")
	}
}
