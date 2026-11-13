// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPanelCSPOnlyAllowsSameOriginAndKeepsCryptoRestrictions(t *testing.T) {
	response := httptest.NewRecorder()
	response.Header().Set("Content-Security-Policy", "default-src 'none'; connect-src 'none'; frame-ancestors 'none'")
	writer := &panelWriter{ResponseWriter: response}
	writer.Write([]byte("trusted app document"))
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "frame-ancestors 'self'") || !strings.Contains(policy, "connect-src 'none'") || response.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatal(policy)
	}
	for _, path := range []string{"/assets/app.css", "/api/messages/raw", "/keys-foreign", "https://foreign.test/keys"} {
		if appPanelPath(path) {
			t.Fatal("non-app document framed", path)
		}
	}
	for _, path := range []string{"/keys", "/secure/compose", "/accounts/new", "/settings"} {
		if !appPanelPath(path) {
			t.Fatal("missing app panel", path)
		}
	}
}
