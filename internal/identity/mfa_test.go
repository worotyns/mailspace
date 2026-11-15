// SPDX-License-Identifier: AGPL-3.0-only
package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const mfaPassword = "Synthetic authenticator test password"

type mfaFixture struct {
	s                    *Store
	now                  time.Time
	path                 string
	alice, bob           User
	aliceToken, bobToken string
	enrollment           MFAEnrollment
	active               MFAResult
}

func newMFAFixture(t *testing.T, enable bool) *mfaFixture {
	t.Helper()
	f := &mfaFixture{path: filepath.Join(t.TempDir(), "auth.db"), now: time.Date(2026, 11, 15, 12, 0, 1, 0, time.UTC)}
	var err error
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.s.Close() })
	f.s.now = func() time.Time { return f.now }
	ctx := context.Background()
	f.alice, err = f.s.CreateUser(ctx, "alice@example.test", mfaPassword)
	if err != nil {
		t.Fatal(err)
	}
	f.bob, err = f.s.CreateUser(ctx, "bob@example.test", mfaPassword)
	if err != nil {
		t.Fatal(err)
	}
	f.aliceToken, _, err = f.s.NewSession(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.bobToken, _, err = f.s.NewSession(ctx, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if enable {
		f.enrollment, err = f.s.BeginMFAEnrollment(ctx, f.alice.ID, f.aliceToken, mfaPassword)
		if err != nil {
			t.Fatal(err)
		}
		f.active, err = f.s.ConfirmMFAEnrollment(ctx, f.alice.ID, f.aliceToken, mfaCode(t, f.enrollment.Secret, f.now))
		if err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func mfaCode(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	code, err := totp.GenerateCodeCustom(secret, now, totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		t.Fatal(err)
	}
	return code
}
func (f *mfaFixture) challenge(t *testing.T) (LoginResult, LoginChallenge) {
	t.Helper()
	ctx := context.Background()
	result, err := f.s.PasswordLogin(ctx, f.alice.Email, mfaPassword)
	if err != nil {
		t.Fatal(err)
	}
	if result.Token != "" || result.Challenge == "" || result.Session.User.ID != "" {
		t.Fatal("password minted a full MFA session")
	}
	if _, err = f.s.Session(ctx, result.Challenge); !errors.Is(err, ErrNotFound) {
		t.Fatal("pending challenge authorizes a full session", err)
	}
	challenge, err := f.s.LoginChallenge(ctx, result.Challenge)
	if err != nil {
		t.Fatal(err)
	}
	return result, challenge
}
func TestMFAEnrollmentBoundariesAndNoPlaintextPersistence(t *testing.T) {
	f := newMFAFixture(t, false)
	ctx := context.Background()
	if _, err := f.s.BeginMFAEnrollment(ctx, f.alice.ID, f.bobToken, mfaPassword); !errors.Is(err, ErrCredentials) {
		t.Fatal("foreign session enrolled factor", err)
	}
	if _, err := f.s.BeginMFAEnrollment(ctx, f.alice.ID, f.aliceToken, "wrong"); !errors.Is(err, ErrCredentials) {
		t.Fatal("wrong password enrolled factor", err)
	}
	if _, err := os.Stat(f.s.authDir); !os.IsNotExist(err) {
		t.Fatal("failed enrollment initialized a secret keyring")
	}
	enrollment, err := f.s.BeginMFAEnrollment(ctx, f.alice.ID, f.aliceToken, mfaPassword)
	if err != nil {
		t.Fatal(err)
	}
	if enrollment.Secret == "" || !strings.HasPrefix(enrollment.URL, "otpauth://totp/") {
		t.Fatal("no interoperable setup metadata")
	}
	if _, err = f.s.PendingMFAEnrollment(ctx, f.alice.ID, f.bobToken); err == nil {
		t.Fatal("foreign session read enrollment")
	}
	second, _, err := f.s.NewSession(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.PendingMFAEnrollment(ctx, f.alice.ID, second); err == nil {
		t.Fatal("another session read pending seed")
	}
	if _, err = f.s.ConfirmMFAEnrollment(ctx, f.alice.ID, second, mfaCode(t, enrollment.Secret, f.now)); err == nil {
		t.Fatal("another session enabled pending seed")
	}
	var ciphertext []byte
	if err = f.s.db.QueryRow("SELECT secret FROM user_mfa WHERE user_id=?", f.alice.ID).Scan(&ciphertext); err != nil || bytes.Contains(ciphertext, []byte(enrollment.Secret)) {
		t.Fatal("seed was not protected", err)
	}
	if _, err = f.s.ConfirmMFAEnrollment(ctx, f.alice.ID, f.aliceToken, "bad"); !errors.Is(err, ErrMFAProof) {
		t.Fatal("bad setup code enabled factor", err)
	}
	status, err := f.s.MFAStatus(ctx, f.alice.ID)
	if err != nil || status.Enabled {
		t.Fatal("failed setup became enabled", err)
	}
	result, err := f.s.ConfirmMFAEnrollment(ctx, f.alice.ID, f.aliceToken, mfaCode(t, enrollment.Secret, f.now))
	if err != nil || len(result.Codes) != 10 {
		t.Fatal("setup failed", err)
	}
	for _, old := range []string{f.aliceToken, second} {
		if _, err = f.s.Session(ctx, old); !errors.Is(err, ErrNotFound) {
			t.Fatal("setup retained old sessions")
		}
	}
	if _, err = f.s.Session(ctx, f.bobToken); err != nil {
		t.Fatal("setup revoked another user's session", err)
	}
	if _, err = f.s.Session(ctx, result.Token); err != nil {
		t.Fatal("setup did not create verified session", err)
	}
	if _, _, err = f.s.NewSession(ctx, f.alice.ID); !errors.Is(err, ErrSecondFactor) {
		t.Fatal("unverified session constructor bypassed MFA", err)
	}
	if _, err = f.s.PendingMFAEnrollment(ctx, f.alice.ID, result.Token); err == nil {
		t.Fatal("enabled seed can be redisplayed")
	}
	if _, err = f.s.BeginMFAEnrollment(ctx, f.alice.ID, result.Token, mfaPassword); !errors.Is(err, ErrMFAConflict) {
		t.Fatal("enabled factor silently replaced", err)
	}
	var hashes []byte
	rows, err := f.s.db.Query("SELECT code_hash FROM mfa_recovery WHERE user_id=?", f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var hash []byte
		if err = rows.Scan(&hash); err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, hash...)
	}
	rows.Close()
	for _, code := range result.Codes {
		if bytes.Contains(hashes, []byte(code)) || strings.Contains(fmt.Sprintf("%v %#v", result, result), code) {
			t.Fatal("recovery code leaked in storage or diagnostics")
		}
	}
	if strings.Contains(fmt.Sprintf("%v %#v", enrollment, enrollment), enrollment.Secret) {
		t.Fatal("seed leaked in diagnostics")
	}
}
func TestMFAFullSessionRequiresSingleUseProofAndBoundCSRF(t *testing.T) {
	f := newMFAFixture(t, true)
	ctx := context.Background()
	login, challenge := f.challenge(t)
	code := mfaCode(t, f.enrollment.Secret, f.now)
	if _, err := f.s.CompleteMFALogin(ctx, login.Challenge, "wrong", MFAFactor{Code: code}); !errors.Is(err, ErrMFACSRF) {
		t.Fatal("challenge CSRF bypass", err)
	}
	if _, err := f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: code}); !errors.Is(err, ErrMFAProof) {
		t.Fatal("setup code replay accepted", err)
	}
	f.now = f.now.Add(30 * time.Second)
	code = mfaCode(t, f.enrollment.Secret, f.now)
	result, err := f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: code})
	if err != nil || result.Token == "" {
		t.Fatal("valid factor did not finish login", err)
	}
	if session, err := f.s.Session(ctx, result.Token); err != nil || session.User.ID != f.alice.ID {
		t.Fatal("wrong completed session", err)
	}
	if _, err = f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: code}); !errors.Is(err, ErrNotFound) {
		t.Fatal("challenge replay accepted", err)
	}
	other, proof := f.challenge(t)
	if _, err = f.s.CompleteMFALogin(ctx, other.Challenge, proof.CSRF, MFAFactor{Code: code}); !errors.Is(err, ErrMFAProof) {
		t.Fatal("TOTP counter reused in another challenge", err)
	}
	if strings.Contains(fmt.Sprint(MFAFactor{Code: code}), code) {
		t.Fatal("factor proof leaked in diagnostics")
	}
}
func TestMFAConcurrentProofAndRecoveryConsumption(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(fmt.Sprint(recovery), func(t *testing.T) {
			f := newMFAFixture(t, true)
			ctx := context.Background()
			f.now = f.now.Add(30 * time.Second)
			first, a := f.challenge(t)
			second, b := f.challenge(t)
			factor := MFAFactor{Code: mfaCode(t, f.enrollment.Secret, f.now)}
			if recovery {
				factor = MFAFactor{Code: f.active.Codes[0], Recovery: true}
			}
			var successes atomic.Int32
			var group sync.WaitGroup
			for _, item := range []struct{ token, csrf string }{{first.Challenge, a.CSRF}, {second.Challenge, b.CSRF}} {
				group.Add(1)
				go func(token, csrf string) {
					defer group.Done()
					if _, err := f.s.CompleteMFALogin(ctx, token, csrf, factor); err == nil {
						successes.Add(1)
					} else if !errors.Is(err, ErrMFAProof) {
						t.Errorf("unexpected concurrent failure: %v", err)
					}
				}(item.token, item.csrf)
			}
			group.Wait()
			if successes.Load() != 1 {
				t.Fatal("proof minted multiple sessions", successes.Load())
			}
			if recovery {
				status, err := f.s.MFAStatus(ctx, f.alice.ID)
				if err != nil || status.RecoveryRemaining != 9 {
					t.Fatal("recovery code did not consume once", err)
				}
			}
		})
	}
}
func TestMFASharedRateBudgetExpiryAndClockSkew(t *testing.T) {
	f := newMFAFixture(t, true)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		login, challenge := f.challenge(t)
		if _, err := f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: "not-a-code"}); !errors.Is(err, ErrMFAProof) {
			t.Fatal("invalid factor not refused", err)
		}
	}
	// Expire the challenge cap independently; persistent failures remain shared.
	if _, err := f.s.db.Exec("DELETE FROM mfa_challenges WHERE user_id=?", f.alice.ID); err != nil {
		t.Fatal(err)
	}
	login, challenge := f.challenge(t)
	f.now = f.now.Add(30 * time.Second)
	if _, err := f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: mfaCode(t, f.enrollment.Secret, f.now)}); !errors.Is(err, ErrMFARate) {
		t.Fatal("new challenge bypassed shared budget", err)
	}
	f.now = f.now.Add(5 * time.Minute)
	if _, err := f.s.LoginChallenge(ctx, login.Challenge); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired challenge accepted", err)
	}
	login, challenge = f.challenge(t)
	if _, err := f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: mfaCode(t, f.enrollment.Secret, f.now.Add(-60*time.Second))}); !errors.Is(err, ErrMFAProof) {
		t.Fatal("clock skew beyond profile accepted", err)
	}
	if _, err := f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: mfaCode(t, f.enrollment.Secret, f.now.Add(-30*time.Second))}); err != nil {
		t.Fatal("allowed adjacent code refused", err)
	}
}
func TestMFAPasswordResetRevokeAndOperatorRecovery(t *testing.T) {
	for _, action := range []string{"password", "revoke", "operator-reset"} {
		t.Run(action, func(t *testing.T) {
			f := newMFAFixture(t, true)
			ctx := context.Background()
			login, challenge := f.challenge(t)
			_, version, err := f.s.passwordProof(ctx, f.alice.Email, mfaPassword)
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "password":
				err = f.s.ResetPassword(ctx, f.alice.Email, "Synthetic replacement password")
			case "revoke":
				err = f.s.RevokeAll(ctx, f.alice.ID)
			default:
				err = f.s.ResetMFA(ctx, f.alice.Email)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.LoginChallenge(ctx, login.Challenge); !errors.Is(err, ErrNotFound) {
				t.Fatal("old challenge survived security change", err)
			}
			if _, err = f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: f.active.Codes[0], Recovery: true}); !errors.Is(err, ErrNotFound) {
				t.Fatal("revoked challenge completed", err)
			}
			if _, err = f.s.loginVerified(ctx, f.alice.ID, version); !errors.Is(err, ErrCredentials) {
				t.Fatal("old password proof minted session after revocation", err)
			}
			if _, err = f.s.Session(ctx, f.active.Token); !errors.Is(err, ErrNotFound) {
				t.Fatal("old full session survived security change", err)
			}
			if _, err = f.s.Session(ctx, f.bobToken); err != nil {
				t.Fatal("other user's session revoked", err)
			}
			status, err := f.s.MFAStatus(ctx, f.alice.ID)
			if err != nil || status.Enabled != (action != "operator-reset") {
				t.Fatal("password/revoke silently changed MFA", err)
			}
		})
	}
}
func TestMFAProofRollsBackWhenSessionCreationFails(t *testing.T) {
	f := newMFAFixture(t, true)
	ctx := context.Background()
	login, challenge := f.challenge(t)
	if _, err := f.s.db.Exec("CREATE TRIGGER fail_mfa_session BEFORE INSERT ON sessions BEGIN SELECT RAISE(ABORT,'synthetic session failure'); END"); err != nil {
		t.Fatal(err)
	}
	factor := MFAFactor{Code: f.active.Codes[0], Recovery: true}
	if _, err := f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, factor); err == nil {
		t.Fatal("session failure ignored")
	}
	if _, err := f.s.db.Exec("DROP TRIGGER fail_mfa_session"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, factor); err != nil {
		t.Fatal("failed transaction consumed recovery proof", err)
	}
}
func TestMFAKeyLossAndExplicitRecoveryWithoutSilentReplacement(t *testing.T) {
	f := newMFAFixture(t, true)
	ctx := context.Background()
	login, challenge := f.challenge(t)
	f.now = f.now.Add(30 * time.Second)
	if err := os.Rename(f.s.authDir, f.s.authDir+"-backup"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: mfaCode(t, f.enrollment.Secret, f.now)}); !errors.Is(err, ErrMFAUnavailable) {
		t.Fatal("missing keyring bypassed or replaced", err)
	}
	if _, err := f.s.BeginMFAEnrollment(ctx, f.bob.ID, f.bobToken, mfaPassword); !errors.Is(err, ErrMFAUnavailable) {
		t.Fatal("another enrollment silently replaced lost keyring", err)
	}
	if _, err := os.Stat(f.s.authDir); !os.IsNotExist(err) {
		t.Fatal("lost keyring recreated")
	}
	result, err := f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: f.active.Codes[0], Recovery: true})
	if err != nil {
		t.Fatal("explicit recovery proof failed after key loss", err)
	}
	if _, err = f.s.DisableMFA(ctx, f.alice.ID, result.Token, mfaPassword, MFAFactor{Code: f.active.Codes[1], Recovery: true}); err != nil {
		t.Fatal("explicit factor recovery failed", err)
	}
}
func TestMFAEnrollmentExpiryRestartAndRecoveryRegeneration(t *testing.T) {
	f := newMFAFixture(t, false)
	ctx := context.Background()
	enrollment, err := f.s.BeginMFAEnrollment(ctx, f.alice.ID, f.aliceToken, mfaPassword)
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(MFAEnrollmentTTL)
	if _, err = f.s.ConfirmMFAEnrollment(ctx, f.alice.ID, f.aliceToken, mfaCode(t, enrollment.Secret, f.now)); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired setup accepted", err)
	}
	enrollment, err = f.s.BeginMFAEnrollment(ctx, f.alice.ID, f.aliceToken, mfaPassword)
	if err != nil {
		t.Fatal(err)
	}
	f.s.Close()
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.s.now = func() time.Time { return f.now }
	pending, err := f.s.PendingMFAEnrollment(ctx, f.alice.ID, f.aliceToken)
	if err != nil || pending.Secret != enrollment.Secret {
		t.Fatal("restart lost protected setup", err)
	}
	result, err := f.s.ConfirmMFAEnrollment(ctx, f.alice.ID, f.aliceToken, mfaCode(t, enrollment.Secret, f.now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RegenerateRecoveryCodes(ctx, f.alice.ID, f.bobToken, mfaPassword, MFAFactor{Code: result.Codes[0], Recovery: true}); err == nil {
		t.Fatal("foreign session regenerated codes")
	}
	changed, err := f.s.RegenerateRecoveryCodes(ctx, f.alice.ID, result.Token, mfaPassword, MFAFactor{Code: result.Codes[0], Recovery: true})
	if err != nil || len(changed.Codes) != 10 {
		t.Fatal("recovery regeneration failed", err)
	}
	login, err := f.s.PasswordLogin(ctx, f.alice.Email, mfaPassword)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := f.s.LoginChallenge(ctx, login.Challenge)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: result.Codes[1], Recovery: true}); !errors.Is(err, ErrMFAProof) {
		t.Fatal("old recovery set survived regeneration", err)
	}
	if _, err = f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: changed.Codes[0], Recovery: true}); err != nil {
		t.Fatal("new recovery proof refused", err)
	}
}

func TestMFATOTPProfileMatchesRFC6238AndBoundsSkew(t *testing.T) {
	// RFC 6238 Appendix B SHA-1 vectors, reduced to the profile's six digits.
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for _, item := range []struct {
		unix int64
		code string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		if actual := matchedTOTPStep(item.code, secret, time.Unix(item.unix, 0)); actual != item.unix/30 {
			t.Fatal("TOTP profile differs from RFC vector", item.unix)
		}
	}
	for _, offset := range []int64{-30, 0, 30} {
		if matchedTOTPStep("005924", secret, time.Unix(1234567890+offset, 0)) != 1234567890/30 {
			t.Fatal("adjacent skew differs from profile", offset)
		}
	}
	for _, offset := range []int64{-60, 60} {
		if matchedTOTPStep("005924", secret, time.Unix(1234567890+offset, 0)) != -1 {
			t.Fatal("excessive skew accepted", offset)
		}
	}
}

func TestMFASeedCiphertextCannotBeTransplantedAcrossUsers(t *testing.T) {
	f := newMFAFixture(t, true)
	ctx := context.Background()
	enrollment, err := f.s.BeginMFAEnrollment(ctx, f.bob.ID, f.bobToken, mfaPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ConfirmMFAEnrollment(ctx, f.bob.ID, f.bobToken, mfaCode(t, enrollment.Secret, f.now)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec("UPDATE user_mfa SET key_id=(SELECT key_id FROM user_mfa WHERE user_id=?),secret=(SELECT secret FROM user_mfa WHERE user_id=?) WHERE user_id=?", f.bob.ID, f.bob.ID, f.alice.ID); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(30 * time.Second)
	login, challenge := f.challenge(t)
	if _, err = f.s.CompleteMFALogin(ctx, login.Challenge, challenge.CSRF, MFAFactor{Code: mfaCode(t, enrollment.Secret, f.now)}); !errors.Is(err, ErrMFAUnavailable) {
		t.Fatal("foreign user's seed ciphertext authorized a factor", err)
	}
}
