// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/webui"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// The endpoint exposes authentication metadata only. It accepts no key, body,
// passphrase or operation argument and does not grant access by its binding.
func cryptoSessionRoutes(mux *http.ServeMux, config AuthConfig, cookieName string, protected protectedRoute) {
	mux.HandleFunc("GET /api/crypto/session", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			http.Error(w, "Authentication required", 401)
			return
		}
		binding := sha256.Sum256([]byte(cookie.Value))
		w.Header().Set("Cache-Control", "no-store")
		reply(w, map[string]any{"userID": s.User.ID, "binding": hex.EncodeToString(binding[:]), "expiresAt": s.ExpiresAt.UnixMilli()})
	}))
	mux.HandleFunc("GET /assets/crypto/session-worker.mjs", protected(func(w http.ResponseWriter, r *http.Request, _ identity.Session) {
		origin, err := url.Parse(config.Origin)
		if err != nil || origin.Host == "" {
			http.Error(w, "Worker unavailable", 503)
			return
		}
		endpoint := origin.Scheme + "://" + origin.Host + "/api/crypto/session"
		// CSP host-source matching excludes literal IPs except 127.0.0.1.
		// For IP origins, self still denies every external destination; the
		// worker code admits only the fixed session-metadata GET.
		if ip := net.ParseIP(origin.Hostname()); ip != nil && origin.Hostname() != "127.0.0.1" {
			endpoint = "'self'"
		}
		// WebKit checks static module imports against worker-src as well as
		// script-src. Admit only this fixed, local dependency graph, not self,
		// blob URLs, arbitrary asset paths or another worker entrypoint.
		modules := []string{"/assets/crypto/vault.mjs", "/assets/crypto/store.mjs", "/assets/crypto/errors.mjs", "/assets/crypto/session-core.mjs", "/assets/vendor/openpgp/openpgp.min.mjs", "/assets/vendor/idb/index.js"}
		for i := range modules {
			modules[i] = origin.Scheme + "://" + origin.Host + modules[i]
		}
		workerSources := strings.Join(modules, " ")
		if endpoint == "'self'" {
			workerSources = "'self'"
		}
		// Worker CSP is independent of the embedding document's network policy.
		// Only a fixed metadata endpoint is permitted; RPC data never enters fetch.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src "+endpoint+"; worker-src "+workerSources+"; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Cache-Control", "no-store")
		webui.Assets(config.Dev).ServeHTTP(w, r)
	}))
}
