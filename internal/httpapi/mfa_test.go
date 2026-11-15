// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"mailspace.local/mailspace/internal/identity"
)

const mfaTestOrigin = "https://mail.example.test"
const mfaTestPassword = "a long synthetic password"

type mfaHTTPTest struct {
	t        *testing.T
	store    *identity.Store
	handler  http.Handler
	requests int
	path     string
}

func newMFAHTTPTest(t *testing.T) *mfaHTTPTest {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mfa.db")
	store, err := identity.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	handler, err := NewPersonal(t.TempDir(), store, AuthConfig{Origin: mfaTestOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &mfaHTTPTest{t: t, store: store, handler: handler, path: path}
}
func (h *mfaHTTPTest) user(email string) (identity.User, *http.Cookie, identity.Session) {
	h.t.Helper()
	u, err := h.store.CreateUser(context.Background(), email, mfaTestPassword)
	if err != nil {
		h.t.Fatal(err)
	}
	token, s, err := h.store.NewSession(context.Background(), u.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return u, &http.Cookie{Name: "__Host-mailspace", Value: token}, s
}
func (h *mfaHTTPTest) request(method, path, origin string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	h.t.Helper()
	h.requests++
	req := httptest.NewRequest(method, mfaTestOrigin+path, strings.NewReader(form.Encode()))
	// Independent synthetic clients exercise the persistent per-user factor budget.
	req.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", h.requests)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, req)
	if response.Header().Get("Cache-Control") != "no-store" {
		h.t.Fatal("security response was cacheable")
	}
	return response
}
func mfaCSRF(t *testing.T, r *httptest.ResponseRecorder) string {
	t.Helper()
	match := regexp.MustCompile(`name="csrf"\s+value="([^"]+)"`).FindStringSubmatch(r.Body.String())
	if len(match) != 2 {
		t.Fatalf("missing CSRF form (status %d)", r.Code)
	}
	return match[1]
}
func mfaCodes(t *testing.T, r *httptest.ResponseRecorder) []string {
	t.Helper()
	codes := regexp.MustCompile(`[A-Z2-7]{8}(?:-[A-Z2-7]{8}){3}`).FindAllString(r.Body.String(), -1)
	if len(codes) != 10 {
		t.Fatalf("expected ten once-only recovery codes; got %d, status %d", len(codes), r.Code)
	}
	return codes
}
func mfaCookie(t *testing.T, r *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range r.Result().Cookies() {
		if c.Name == name && c.MaxAge > 0 {
			if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
				t.Fatal("insecure authentication cookie")
			}
			return c
		}
	}
	t.Fatalf("missing authentication cookie (status %d)", r.Code)
	return nil
}
func mfaStatus(t *testing.T, r *httptest.ResponseRecorder, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("status %d; want %d", r.Code, want)
	}
}
func (h *mfaHTTPTest) begin(cookie *http.Cookie, csrf string) string {
	h.t.Helper()
	mfaStatus(h.t, h.request("POST", "/security/authenticator/setup", mfaTestOrigin, url.Values{"csrf": {csrf}, "password": {mfaTestPassword}}, cookie), 303)
	r := h.request("GET", "/security", "", nil, cookie)
	mfaStatus(h.t, r, 200)
	match := regexp.MustCompile(`<code[^>]*>\s*([A-Z2-7]+)\s*</code\s*>`).FindStringSubmatch(r.Body.String())
	if len(match) != 2 {
		h.t.Fatal("missing session-bound manual setup key")
	}
	return match[1]
}
func TestAuthenticatorHTTPEnrollmentLoginRecoveryAndDisable(t *testing.T) {
	h := newMFAHTTPTest(t)
	ctx := context.Background()
	alice, old, s := h.user("alice@example.test")
	_, bob, bs := h.user("bob@example.test")
	otherToken, _, err := h.store.NewSession(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameOwner := &http.Cookie{Name: old.Name, Value: otherToken}
	mfaStatus(t, h.request("POST", "/security/authenticator/setup", "https://foreign.test", url.Values{"csrf": {s.CSRF}, "password": {mfaTestPassword}}, old), 403)
	mfaStatus(t, h.request("POST", "/security/authenticator/setup", mfaTestOrigin, url.Values{"csrf": {"wrong"}, "password": {mfaTestPassword}}, old), 403)
	wrongPassword := h.request("POST", "/security/authenticator/setup", mfaTestOrigin, url.Values{"csrf": {s.CSRF}, "password": {"incorrect password"}}, old)
	mfaStatus(t, wrongPassword, 400)
	if !strings.Contains(wrongPassword.Body.String(), "Current Mailspace sign-in password not accepted") || strings.Contains(wrongPassword.Body.String(), "Use a fresh Authenticator code") {
		t.Fatal("setup password error requests a nonexistent app/recovery code")
	}
	secret := h.begin(old, s.CSRF)
	for _, c := range []*http.Cookie{nil, bob, sameOwner} {
		r := h.request("GET", "/security/authenticator/qr", "", nil, c)
		if r.Code == 200 || strings.Contains(r.Body.String(), secret) {
			t.Fatal("setup escaped its owner/session")
		}
	}
	bobPage := h.request("GET", "/security", "", nil, bob)
	mfaStatus(t, bobPage, 200)
	if strings.Contains(bobPage.Body.String(), secret) {
		t.Fatal("seed leaked between users")
	}
	qr := h.request("GET", "/security/authenticator/qr", "", nil, old)
	mfaStatus(t, qr, 200)
	image, err := png.DecodeConfig(qr.Body)
	if err != nil || image.Width != 256 || image.Height != 256 || qr.Header().Get("Content-Type") != "image/png" {
		t.Fatal("expected local bounded PNG QR")
	}
	mfaStatus(t, h.request("POST", "/security/authenticator/enable", mfaTestOrigin, url.Values{"csrf": {s.CSRF}, "code": {"invalid"}}, old), 400)
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	enabled := h.request("POST", "/security/authenticator/enable", mfaTestOrigin, url.Values{"csrf": {s.CSRF}, "code": {code}}, old)
	mfaStatus(t, enabled, 200)
	codes := mfaCodes(t, enabled)
	full := mfaCookie(t, enabled, old.Name)
	csrf := mfaCSRF(t, enabled)
	for _, c := range []*http.Cookie{old, sameOwner} {
		mfaStatus(t, h.request("GET", "/api/session", "", nil, c), 401)
	}
	mfaStatus(t, h.request("GET", "/api/session", "", nil, bob), 200)
	mfaStatus(t, h.request("GET", "/security/authenticator/qr", "", nil, full), 404)
	page := h.request("GET", "/security", "", nil, full)
	for _, value := range append(codes, secret) {
		if strings.Contains(page.Body.String(), value) {
			t.Fatal("enrolled seed/recovery codes redisplayed")
		}
	}
	// Password succeeds only into an unprivileged cookie, never a full session.
	login := h.request("POST", "/auth/login", mfaTestOrigin, url.Values{"email": {alice.Email}, "password": {mfaTestPassword}}, nil)
	mfaStatus(t, login, 303)
	if login.Header().Get("Location") != "/auth/mfa" {
		t.Fatal("missing second login step")
	}
	pending := mfaCookie(t, login, old.Name+"-mfa")
	for _, c := range login.Result().Cookies() {
		if c.Name == old.Name && c.MaxAge > 0 {
			t.Fatal("password bypassed MFA")
		}
	}
	mfaStatus(t, h.request("GET", "/api/session", "", nil, &http.Cookie{Name: old.Name, Value: pending.Value}), 401)
	for _, path := range []string{"/api/session", "/api/accounts", "/keys", "/security", "/"} {
		r := h.request("GET", path, "", nil, pending)
		if r.Code == 200 {
			t.Fatalf("pending cookie authorized %s", path)
		}
	}
	challenge := h.request("GET", "/auth/mfa", "", nil, pending)
	mfaStatus(t, challenge, 200)
	challengeCSRF := mfaCSRF(t, challenge)
	proof := url.Values{"csrf": {challengeCSRF}, "proof_type": {"recovery"}, "code": {codes[0]}}
	mfaStatus(t, h.request("POST", "/auth/mfa", "https://foreign.test", proof, pending), 403)
	proof.Set("csrf", bs.CSRF)
	mfaStatus(t, h.request("POST", "/auth/mfa", mfaTestOrigin, proof, pending), 403)
	proof.Set("csrf", challengeCSRF)
	proof.Set("proof_type", "unknown")
	mfaStatus(t, h.request("POST", "/auth/mfa", mfaTestOrigin, proof, pending), 400)
	proof.Set("proof_type", "totp")
	proof.Set("code", code) // Enrollment already consumed this TOTP step.
	mfaStatus(t, h.request("POST", "/auth/mfa", mfaTestOrigin, proof, pending), 400)
	proof.Set("proof_type", "recovery")
	proof.Set("code", codes[0])
	verified := h.request("POST", "/auth/mfa", mfaTestOrigin, proof, pending)
	mfaStatus(t, verified, 303)
	newFull := mfaCookie(t, verified, old.Name)
	mfaStatus(t, h.request("GET", "/api/session", "", nil, newFull), 200)
	replay := h.request("POST", "/auth/mfa", mfaTestOrigin, proof, pending)
	mfaStatus(t, replay, 303)
	for _, c := range replay.Result().Cookies() {
		if c.Name == old.Name && c.MaxAge > 0 {
			t.Fatal("replayed challenge issued a session")
		}
	}
	// Regeneration reauthenticates, invalidates previous codes and rotates sessions.
	regenForm := url.Values{"csrf": {csrf}, "password": {mfaTestPassword}, "proof_type": {"recovery"}, "code": {codes[1]}}
	regenForm.Set("password", "wrong")
	mfaStatus(t, h.request("POST", "/security/authenticator/recovery", mfaTestOrigin, regenForm, full), 400)
	regenForm.Set("password", mfaTestPassword)
	regenerated := h.request("POST", "/security/authenticator/recovery", mfaTestOrigin, regenForm, full)
	mfaStatus(t, regenerated, 200)
	replacement := mfaCodes(t, regenerated)
	rotated := mfaCookie(t, regenerated, old.Name)
	rotatedCSRF := mfaCSRF(t, regenerated)
	mfaStatus(t, h.request("GET", "/api/session", "", nil, full), 401)
	mfaStatus(t, h.request("GET", "/api/session", "", nil, newFull), 401)
	disable := url.Values{"csrf": {rotatedCSRF}, "password": {mfaTestPassword}, "proof_type": {"recovery"}, "code": {codes[2]}}
	mfaStatus(t, h.request("POST", "/security/authenticator/disable", mfaTestOrigin, disable, rotated), 400)
	disable.Set("code", replacement[0])
	disabled := h.request("POST", "/security/authenticator/disable", mfaTestOrigin, disable, rotated)
	mfaStatus(t, disabled, 200)
	mfaCookie(t, disabled, old.Name)
	status, err := h.store.MFAStatus(ctx, alice.ID)
	if err != nil || status.Enabled {
		t.Fatal("disable failed")
	}
	mfaStatus(t, h.request("GET", "/api/session", "", nil, bob), 200)
	ordinary := h.request("POST", "/auth/login", mfaTestOrigin, url.Values{"email": {alice.Email}, "password": {mfaTestPassword}}, nil)
	mfaStatus(t, ordinary, 303)
	mfaCookie(t, ordinary, old.Name)
}

func TestAuthenticatorHTTPChallengeCancellationAndBudgets(t *testing.T) {
	h := newMFAHTTPTest(t)
	ctx := context.Background()
	u, cookie, s := h.user("challenge@example.test")
	secret := h.begin(cookie, s.CSRF)
	code, _ := totp.GenerateCode(secret, time.Now())
	active, err := h.store.ConfirmMFAEnrollment(ctx, u.ID, cookie.Value, code)
	if err != nil {
		t.Fatal(err)
	}
	login, err := h.store.PasswordLogin(ctx, u.Email, mfaTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	pending := &http.Cookie{Name: cookie.Name + "-mfa", Value: login.Challenge}
	page := h.request("GET", "/auth/mfa", "", nil, pending)
	csrf := mfaCSRF(t, page)
	mfaStatus(t, h.request("POST", "/auth/mfa/cancel", mfaTestOrigin, url.Values{"csrf": {"wrong"}}, pending), 403)
	mfaStatus(t, h.request("POST", "/auth/mfa/cancel", mfaTestOrigin, url.Values{"csrf": {csrf}}, pending), 303)
	if _, err = h.store.LoginChallenge(ctx, pending.Value); err == nil {
		t.Fatal("cancel did not revoke challenge")
	}
	login, err = h.store.PasswordLogin(ctx, u.Email, mfaTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	pending.Value = login.Challenge
	page = h.request("GET", "/auth/mfa", "", nil, pending)
	csrf = mfaCSRF(t, page)
	invalid := url.Values{"csrf": {csrf}, "proof_type": {"totp"}, "code": {"invalid"}}
	for i := 0; i < 5; i++ {
		r := h.request("POST", "/auth/mfa", mfaTestOrigin, invalid, pending)
		if r.Code != 400 && r.Code != 303 {
			t.Fatalf("unexpected invalid-proof status %d", r.Code)
		}
	}
	login, err = h.store.PasswordLogin(ctx, u.Email, mfaTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	pending.Value = login.Challenge
	page = h.request("GET", "/auth/mfa", "", nil, pending)
	valid := url.Values{"csrf": {mfaCSRF(t, page)}, "proof_type": {"recovery"}, "code": {active.Codes[0]}}
	blocked := h.request("POST", "/auth/mfa", mfaTestOrigin, valid, pending)
	mfaStatus(t, blocked, 429)
	if blocked.Header().Get("Retry-After") != "300" {
		t.Fatal("missing shared rate guidance")
	}
	for _, c := range blocked.Result().Cookies() {
		if c.Name == cookie.Name && c.MaxAge > 0 {
			t.Fatal("shared budget bypassed")
		}
	}
	// A pending cookie cannot invoke factor management or the local operator reset.
	for _, path := range []string{"/security/authenticator/disable", "/security/authenticator/recovery", "/auth/reset-2fa"} {
		r := h.request("POST", path, mfaTestOrigin, valid, pending)
		if r.Code == 200 {
			t.Fatal("pending login mutated security")
		}
	}
}

func TestAuthenticatorHTTPPendingCancelAndBoundedForms(t *testing.T) {
	h := newMFAHTTPTest(t)
	ctx := context.Background()
	u, cookie, s := h.user("setup@example.test")
	secret := h.begin(cookie, s.CSRF)
	mfaStatus(t, h.request("POST", "/security/authenticator/cancel", mfaTestOrigin, url.Values{"csrf": {s.CSRF}}, cookie), 303)
	if _, err := h.store.PendingMFAEnrollment(ctx, u.ID, cookie.Value); err == nil {
		t.Fatal("cancel left setup readable")
	}
	page := h.request("GET", "/security", "", nil, cookie)
	if strings.Contains(page.Body.String(), secret) {
		t.Fatal("cancel redisplayed secret")
	}
	oversized := url.Values{"csrf": {s.CSRF}, "password": {strings.Repeat("x", 8192)}}
	mfaStatus(t, h.request("POST", "/security/authenticator/setup", mfaTestOrigin, oversized, cookie), 400)
	// A challenge value substituted as a full-session cookie still authorizes nothing.
	mfaStatus(t, h.request("GET", "/api/session", "", nil, &http.Cookie{Name: cookie.Name, Value: "invented-pending-token"}), 401)
}

func TestAuthenticatorHTTPAcceptsFreshAppCodeAndRotatesExistingSession(t *testing.T) {
	h := newMFAHTTPTest(t)
	ctx := context.Background()
	alice, cookie, s := h.user("app@example.test")
	_, bob, _ := h.user("other@example.test")
	secret := h.begin(cookie, s.CSRF)
	code, _ := totp.GenerateCode(secret, time.Now())
	if _, err := h.store.ConfirmMFAEnrollment(ctx, alice.ID, cookie.Value, code); err != nil {
		t.Fatal(err)
	}
	// A currently signed-in different user must lose that session on account switch.
	req := httptest.NewRequest("POST", mfaTestOrigin+"/auth/login", strings.NewReader(url.Values{"email": {alice.Email}, "password": {mfaTestPassword}}.Encode()))
	req.Header.Set("Origin", mfaTestOrigin)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(bob)
	response := httptest.NewRecorder()
	h.handler.ServeHTTP(response, req)
	mfaStatus(t, response, 303)
	pending := mfaCookie(t, response, cookie.Name+"-mfa")
	cleared := false
	for _, c := range response.Result().Cookies() {
		if c.Name == cookie.Name && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("account switch retained previous browser session cookie")
	}
	if _, err := h.store.Session(ctx, bob.Value); !errors.Is(err, identity.ErrNotFound) {
		t.Fatal("account switch retained previous session")
	}
	challenge := h.request("GET", "/auth/mfa", "", nil, pending)
	// The allowed adjacent step is a fresh counter, independent of enrollment's
	// consumed current code; the deterministic store suite covers clock edges.
	next, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	verified := h.request("POST", "/auth/mfa", mfaTestOrigin, url.Values{"csrf": {mfaCSRF(t, challenge)}, "proof_type": {"totp"}, "code": {next}}, pending)
	mfaStatus(t, verified, 303)
	full := mfaCookie(t, verified, cookie.Name)
	session, err := h.store.Session(ctx, full.Value)
	if err != nil || session.User.ID != alice.ID {
		t.Fatal("app proof did not authorize the intended user")
	}
}

func TestAuthenticatorHTTPKeyLossRequiresExplicitRecovery(t *testing.T) {
	h := newMFAHTTPTest(t)
	ctx := context.Background()
	alice, cookie, s := h.user("lost-key@example.test")
	secret := h.begin(cookie, s.CSRF)
	code, _ := totp.GenerateCode(secret, time.Now())
	active, err := h.store.ConfirmMFAEnrollment(ctx, alice.ID, cookie.Value, code)
	if err != nil {
		t.Fatal(err)
	}
	login, err := h.store.PasswordLogin(ctx, alice.Email, mfaTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	pending := &http.Cookie{Name: cookie.Name + "-mfa", Value: login.Challenge}
	page := h.request("GET", "/auth/mfa", "", nil, pending)
	csrf := mfaCSRF(t, page)
	if err := os.RemoveAll(filepath.Join(filepath.Dir(h.path), "auth-keys")); err != nil {
		t.Fatal(err)
	}
	unavailable := h.request("POST", "/auth/mfa", mfaTestOrigin, url.Values{"csrf": {csrf}, "proof_type": {"totp"}, "code": {code}}, pending)
	mfaStatus(t, unavailable, 503)
	if !strings.Contains(unavailable.Body.String(), "One-time recovery code") {
		t.Fatal("key loss hid explicit recovery form")
	}
	for _, c := range unavailable.Result().Cookies() {
		if c.Name == cookie.Name && c.MaxAge > 0 {
			t.Fatal("key loss bypassed second factor")
		}
	}
	verified := h.request("POST", "/auth/mfa", mfaTestOrigin, url.Values{"csrf": {csrf}, "proof_type": {"recovery"}, "code": {active.Codes[0]}}, pending)
	mfaStatus(t, verified, 303)
	mfaCookie(t, verified, cookie.Name)

	full := mfaCookie(t, verified, cookie.Name)
	security := h.request("GET", "/security", "", nil, full)
	unavailableChange := h.request("POST", "/security/authenticator/disable", mfaTestOrigin, url.Values{"csrf": {mfaCSRF(t, security)}, "password": {mfaTestPassword}, "proof_type": {"totp"}, "code": {code}}, full)
	mfaStatus(t, unavailableChange, 503)
	if !strings.Contains(unavailableChange.Body.String(), `action="/security/authenticator/disable"`) || !strings.Contains(unavailableChange.Body.String(), "One-time recovery code") {
		t.Fatal("security error hid the explicit recovery form")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(h.path), "auth-keys")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("login regenerated lost auth keyring")
	}
}
