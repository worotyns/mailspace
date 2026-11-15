// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"net/http"
	"strings"
)

// Only trusted application documents may be embedded by this same origin.
// Mail HTML keeps its separate opaque sandbox. No content/connect policy relaxed.
type panelWriter struct {
	http.ResponseWriter
	written bool
}

func (p *panelWriter) prepare() {
	if p.written {
		return
	}
	p.written = true
	h := p.Header()
	h.Set("Content-Security-Policy", strings.ReplaceAll(h.Get("Content-Security-Policy"), "frame-ancestors 'none'", "frame-ancestors 'self'"))
	h.Set("X-Frame-Options", "SAMEORIGIN")
}
func (p *panelWriter) WriteHeader(code int)          { p.prepare(); p.ResponseWriter.WriteHeader(code) }
func (p *panelWriter) Write(raw []byte) (int, error) { p.prepare(); return p.ResponseWriter.Write(raw) }
func appPanelPath(path string) bool {
	switch path {
	case "/", "/security", "/security/authenticator/setup", "/security/authenticator/enable", "/security/authenticator/cancel", "/security/authenticator/recovery", "/security/authenticator/disable":
		return true
	}
	for _, prefix := range []string{"/settings", "/profile", "/workspaces", "/accounts", "/keys", "/rules", "/labels", "/sharing", "/attachments", "/automation", "/secure/compose", "/messages/read", "/outbox", "/archive"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
