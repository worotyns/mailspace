// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrivacyResponseHeaders(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reply(w, map[string]string{"status": "ok"}) }))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	for _, key := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "Cache-Control"} {
		if response.Header().Get(key) == "" {
			t.Fatal("missing header", key)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private responses can be cached")
	}
}
