// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"image/png"
	"net"
	"net/http"
	"time"

	"github.com/pquerna/otp"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/webui"
)

func authCookie(w http.ResponseWriter, name string, secure bool, token string, expiry time.Time, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: token, Path: "/", Secure: secure, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: expiry, MaxAge: int(ttl.Seconds())})
}
func expireAuthCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: name, Path: "/", Secure: secure, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: time.Unix(1, 0), MaxAge: -1})
}
func mfaFormFactor(r *http.Request) (identity.MFAFactor, bool) {
	method := r.PostForm.Get("proof_type")
	return identity.MFAFactor{Code: r.PostForm.Get("code"), Recovery: method == "recovery"}, method == "totp" || method == "recovery"
}

func mfaRoutes(mux *http.ServeMux, config AuthConfig, store *identity.Store,
	protected func(func(http.ResponseWriter, *http.Request, identity.Session)) http.HandlerFunc,
	mutation func(http.ResponseWriter, *http.Request, identity.Session) bool,
	cookieName string, secure bool, origin string, limiter *loginLimiter, slots chan struct{}) {
	pendingName := cookieName + "-mfa"
	render := func(w http.ResponseWriter, status int, name string, data webui.AuthPageData) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Buffer template errors before writing a successful/sensitive response.
		var response bytes.Buffer
		capture := &authPageBuffer{HeaderMap: http.Header{}, Buffer: &response}
		if err := webui.RenderAuth(capture, config.Dev, name, data); err != nil {
			http.Error(w, "Security view unavailable", 503)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write(response.Bytes())
	}
	// Apply the body limit before the ordinary protected mutation guard parses it.
	gate := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Origin") != origin {
			http.Error(w, "Origin not allowed", 403)
			return false
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !limiter.allow("mfa:"+host, time.Now()) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too many security attempts", 429)
			return false
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid security form", 400)
			return false
		}
		return true
	}
	securityData := func(r *http.Request, s identity.Session) (webui.AuthPageData, error) {
		data := webui.AuthPageData{Email: s.User.Email, CSRF: s.CSRF}
		status, err := store.MFAStatus(r.Context(), s.User.ID)
		if err != nil {
			return data, err
		}
		data.Security.Enabled = status.Enabled
		data.Security.Remaining = status.RecoveryRemaining
		if status.Pending && !status.Enabled {
			cookie, _ := r.Cookie(cookieName)
			enrollment, err := store.PendingMFAEnrollment(r.Context(), s.User.ID, cookie.Value)
			if err == nil {
				data.Security.Pending = true
				data.Security.EnrollmentSecret = enrollment.Secret
			} else if errors.Is(err, identity.ErrMFAUnavailable) {
				data.Security.Pending = true
				data.Security.SetupUnavailable = true
			} else if !errors.Is(err, identity.ErrNotFound) && !errors.Is(err, identity.ErrCredentials) {
				return data, err
			}
		}
		return data, nil
	}
	securityPage := func(w http.ResponseWriter, r *http.Request, s identity.Session, status int, message string, codes identity.RecoveryCodes) {
		data, err := securityData(r, s)
		if err != nil {
			http.Error(w, "Authenticator setup unavailable; restore auth-keys or cancel the pending setup", 503)
			return
		}
		data.Message = message
		data.Security.RecoveryCodes = codes
		render(w, status, "security", data)
	}
	mux.HandleFunc("GET /security", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) { securityPage(w, r, s, 200, "", nil) }))
	mux.HandleFunc("GET /security/authenticator/qr", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		cookie, _ := r.Cookie(cookieName)
		enrollment, err := store.PendingMFAEnrollment(r.Context(), s.User.ID, cookie.Value)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		key, err := otp.NewKeyFromURL(enrollment.URL)
		if err != nil {
			http.Error(w, "QR unavailable", 503)
			return
		}
		image, err := key.Image(256, 256)
		if err != nil {
			http.Error(w, "QR unavailable", 503)
			return
		}
		var buffer bytes.Buffer
		if err = png.Encode(&buffer, image); err != nil {
			http.Error(w, "QR unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(buffer.Bytes())
	}))
	for _, action := range []string{"setup", "enable", "cancel", "recovery", "disable"} {
		mux.HandleFunc("POST /security/authenticator/"+action, protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
			if !gate(w, r) || !mutation(w, r, s) {
				return
			}
			// Password proofs share the login concurrency budget.
			if action == "setup" || action == "recovery" || action == "disable" {
				select {
				case slots <- struct{}{}:
					defer func() { <-slots }()
				default:
					w.Header().Set("Retry-After", "1")
					http.Error(w, "Security checks busy; try again", 503)
					return
				}
			}
			cookie, _ := r.Cookie(cookieName)
			var result identity.MFAResult
			var err error
			switch action {
			case "setup":
				_, err = store.BeginMFAEnrollment(r.Context(), s.User.ID, cookie.Value, r.PostForm.Get("password"))
			case "cancel":
				err = store.CancelMFAEnrollment(r.Context(), s.User.ID, cookie.Value)
			case "enable":
				result, err = store.ConfirmMFAEnrollment(r.Context(), s.User.ID, cookie.Value, r.PostForm.Get("code"))
			default:
				factor, valid := mfaFormFactor(r)
				if !valid {
					http.Error(w, "Choose Authenticator or recovery code", 400)
					return
				}
				if action == "recovery" {
					result, err = store.RegenerateRecoveryCodes(r.Context(), s.User.ID, cookie.Value, r.PostForm.Get("password"), factor)
				} else {
					result, err = store.DisableMFA(r.Context(), s.User.ID, cookie.Value, r.PostForm.Get("password"), factor)
				}
			}
			if err != nil {
				status, message := mfaFailure(err)
				if action == "setup" && errors.Is(err, identity.ErrCredentials) {
					message = "Current Mailspace sign-in password not accepted. Enter the password used to sign in to Mailspace."
				}
				if status == 429 {
					w.Header().Set("Retry-After", "300")
				}
				// Keep the explicit recovery/cancel forms available on key loss.
				securityPage(w, r, s, status, message, nil)
				return
			}
			if result.Token == "" {
				http.Redirect(w, r, "/security", 303)
				return
			}
			authCookie(w, cookieName, secure, result.Token, result.Session.ExpiresAt, identity.SessionTTL)
			message := "Authenticator enabled. Save your recovery codes now; they are shown only once. All other sessions were signed out."
			if action == "recovery" {
				message = "New recovery codes created. The previous codes no longer work. All other sessions were signed out."
			}
			if action == "disable" {
				message = "Authenticator disabled. All other sessions were signed out."
			}
			securityPage(w, r, result.Session, 200, message, result.Codes)
		}))
	}
	challengePage := func(w http.ResponseWriter, r *http.Request, status int, message string) {
		cookie, err := r.Cookie(pendingName)
		if err != nil {
			http.Redirect(w, r, "/login", 303)
			return
		}
		challenge, err := store.LoginChallenge(r.Context(), cookie.Value)
		if err != nil {
			if !errors.Is(err, identity.ErrNotFound) {
				http.Error(w, "Sign-in service unavailable", 503)
				return
			}
			expireAuthCookie(w, pendingName, secure)
			http.Redirect(w, r, "/login", 303)
			return
		}
		render(w, status, "mfa-login", webui.AuthPageData{CSRF: challenge.CSRF, Message: message, Security: webui.SecurityPageData{RecoveryMethod: r.PostForm.Get("proof_type") == "recovery"}})
	}
	mux.HandleFunc("GET /auth/mfa", func(w http.ResponseWriter, r *http.Request) { challengePage(w, r, 200, "") })
	mux.HandleFunc("POST /auth/mfa", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r) {
			return
		}
		cookie, err := r.Cookie(pendingName)
		if err != nil {
			http.Error(w, "Start sign-in again", 401)
			return
		}
		factor, valid := mfaFormFactor(r)
		if !valid {
			http.Error(w, "Choose Authenticator or recovery code", 400)
			return
		}
		result, err := store.CompleteMFALogin(r.Context(), cookie.Value, r.PostForm.Get("csrf"), factor)
		if err != nil {
			if errors.Is(err, identity.ErrNotFound) {
				expireAuthCookie(w, pendingName, secure)
				http.Redirect(w, r, "/login", 303)
				return
			}
			status, message := mfaFailure(err)
			if status == 429 {
				w.Header().Set("Retry-After", "300")
			}
			if status == 403 {
				http.Error(w, message, status)
				return
			}
			challengePage(w, r, status, message)
			return
		}
		if old, err := r.Cookie(cookieName); err == nil {
			if err = store.RevokeSession(r.Context(), old.Value); err != nil {
				http.Error(w, "Session rotation failed", 503)
				return
			}
		}
		expireAuthCookie(w, pendingName, secure)
		authCookie(w, cookieName, secure, result.Token, result.Session.ExpiresAt, identity.SessionTTL)
		http.Redirect(w, r, "/", 303)
	})
	mux.HandleFunc("POST /auth/mfa/cancel", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w, r) {
			return
		}
		cookie, err := r.Cookie(pendingName)
		if err != nil {
			http.Error(w, "Start sign-in again", 401)
			return
		}
		challenge, err := store.LoginChallenge(r.Context(), cookie.Value)
		if err != nil && !errors.Is(err, identity.ErrNotFound) {
			http.Error(w, "Sign-in service unavailable", 503)
			return
		}
		if err == nil {
			if subtle.ConstantTimeCompare([]byte(challenge.CSRF), []byte(r.PostForm.Get("csrf"))) != 1 {
				http.Error(w, "CSRF token required", 403)
				return
			}
			if err = store.RevokeMFALogin(r.Context(), cookie.Value); err != nil {
				http.Error(w, "Challenge cancellation failed", 503)
				return
			}
		}
		expireAuthCookie(w, pendingName, secure)
		http.Redirect(w, r, "/login", 303)
	})
}

// authPageBuffer allows RenderAuth to finish before writing status or secrets.
type authPageBuffer struct {
	HeaderMap http.Header
	*bytes.Buffer
}

func (b *authPageBuffer) Header() http.Header { return b.HeaderMap }
func (b *authPageBuffer) WriteHeader(int)     {}
func mfaFailure(err error) (int, string) {
	switch {
	case errors.Is(err, identity.ErrMFACSRF):
		return 403, "CSRF token required"
	case errors.Is(err, identity.ErrMFARate):
		return 429, "Too many code attempts. Wait five minutes before trying again."
	case errors.Is(err, identity.ErrMFAUnavailable):
		return 503, "Authenticator secret unavailable. Explicitly select an unused recovery code, or ask the administrator to restore auth-keys."
	case errors.Is(err, identity.ErrMFAProof), errors.Is(err, identity.ErrCredentials):
		return 400, "Password or code not accepted. Use a fresh Authenticator code or an unused recovery code."
	case errors.Is(err, identity.ErrNotFound), errors.Is(err, identity.ErrMFAConflict):
		return 400, "This setup or security action is no longer current. Open Security and try again."
	default:
		return 503, "Security service unavailable. Try again later."
	}
}
