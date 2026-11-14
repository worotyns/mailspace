// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"encoding/json"
	"mailspace.local/mailspace/internal/identity"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestCryptoSessionBindingIsAuthenticatedPerLoginAndNeverRawCookie(t *testing.T) {
	store, err := identity.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	user, err := store.CreateUser(ctx, "crypto@example.test", "Synthetic session password")
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := store.NewSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.NewSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPersonal(t.TempDir(), store, AuthConfig{Origin: "https://mail.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	request := func(path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("/api/crypto/session", ""); w.Code != 401 {
		t.Fatal("anonymous crypto session", w.Code)
	}
	binding := func(token string) string {
		w := request("/api/crypto/session", token)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), token) {
			t.Fatal("unsafe session metadata", w.Code)
		}
		var data struct {
			UserID, Binding string
			ExpiresAt       int64
		}
		if json.Unmarshal(w.Body.Bytes(), &data) != nil || data.UserID != user.ID || len(data.Binding) != 64 || data.ExpiresAt == 0 {
			t.Fatal("invalid metadata")
		}
		return data.Binding
	}
	other, err := store.CreateUser(ctx, "other-worker@example.test", "Synthetic second worker password")
	if err != nil {
		t.Fatal(err)
	}
	otherToken, _, err := store.NewSession(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherResponse := request("/api/crypto/session", otherToken)
	var otherData struct{ UserID string }
	if json.Unmarshal(otherResponse.Body.Bytes(), &otherData) != nil || otherData.UserID != other.ID || strings.Contains(otherResponse.Body.String(), user.ID) {
		t.Fatal("crypto session crossed owners")
	}
	if binding(first) == binding(second) {
		t.Fatal("new login reused crypto binding")
	}
	if err = store.RevokeSession(ctx, first); err != nil {
		t.Fatal(err)
	}
	if w := request("/api/crypto/session", first); w.Code != 401 {
		t.Fatal("revoked session retained crypto access")
	}
}
func TestCryptoWorkerCSPHasOnlyFixedSessionEgress(t *testing.T) {
	store, err := identity.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	user, err := store.CreateUser(context.Background(), "worker@example.test", "Synthetic worker password")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.NewSession(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPersonal(t.TempDir(), store, AuthConfig{Origin: "https://mail.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/assets/crypto/session-worker.mjs", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-mailspace", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	policy := w.Header().Get("Content-Security-Policy")
	if w.Code != 200 || !strings.Contains(policy, "connect-src https://mail.example.test/api/crypto/session;") || !strings.Contains(policy, "worker-src 'none'") || strings.Contains(policy, "unsafe-") {
		t.Fatal("unsafe worker CSP", policy)
	}
}

func TestCryptoWorkerLiteralIPOriginUsesSelfWithoutExternalEgress(t *testing.T) {
	store, err := identity.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	user, err := store.CreateUser(context.Background(), "ipv6@example.test", "Synthetic worker password")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.NewSession(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"http://[::1]:18086", "https://[2001:db8::1]", "https://192.0.2.1"} {
		handler, err := NewPersonal(t.TempDir(), store, AuthConfig{Origin: origin, InsecureLoopback: strings.HasPrefix(origin, "http:")})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("GET", origin+"/assets/crypto/session-worker.mjs", nil)
		cookie := "__Host-mailspace"
		if strings.HasPrefix(origin, "http:") {
			cookie = "mailspace_dev_session"
		}
		request.AddCookie(&http.Cookie{Name: cookie, Value: token})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Security-Policy"), "connect-src 'self';") {
			t.Fatalf("IP worker policy: %d %s", response.Code, response.Header().Get("Content-Security-Policy"))
		}
	}
}
