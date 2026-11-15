// SPDX-License-Identifier: AGPL-3.0-only
package identity

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

type LoginResult struct {
	Token, Challenge string
	Session          Session
	ExpiresAt        time.Time
}

func (LoginResult) String() string   { return "<redacted login result>" }
func (LoginResult) GoString() string { return "<redacted login result>" }

type LoginChallenge struct {
	CSRF      string
	ExpiresAt time.Time
}

func (LoginChallenge) String() string   { return "<redacted login challenge>" }
func (LoginChallenge) GoString() string { return "<redacted login challenge>" }
func equalDigest(a, b []byte) bool      { return subtle.ConstantTimeCompare(a, b) == 1 }

// PasswordLogin issues either a full session or an unprivileged challenge. The
// verified password version is rechecked inside the creation transaction.
func (s *Store) PasswordLogin(ctx context.Context, email, password string) (LoginResult, error) {
	user, version, err := s.passwordProof(ctx, email, password)
	if err != nil {
		return LoginResult{}, err
	}
	return s.loginVerified(ctx, user.ID, version)
}
func (s *Store) loginVerified(ctx context.Context, user string, version int64) (LoginResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LoginResult{}, err
	}
	defer tx.Rollback()
	var current int64
	if err = tx.QueryRowContext(ctx, "SELECT auth_version FROM users WHERE id=?", user).Scan(&current); err != nil {
		return LoginResult{}, err
	}
	if version != current {
		return LoginResult{}, ErrCredentials
	}
	var enabled bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM user_mfa WHERE user_id=? AND enabled=1)", user).Scan(&enabled); err != nil {
		return LoginResult{}, err
	}
	if !enabled {
		token, session, err := s.newSessionTx(ctx, tx, user)
		if err != nil {
			return LoginResult{}, err
		}
		if err = tx.Commit(); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{Token: token, Session: session, ExpiresAt: session.ExpiresAt}, nil
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM mfa_challenges WHERE expires_at<=?", s.now().Unix()); err != nil {
		return LoginResult{}, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM mfa_challenges WHERE user_id=?", user).Scan(&count); err != nil {
		return LoginResult{}, err
	}
	if count >= 5 {
		return LoginResult{}, ErrMFARate
	}
	token := randomToken()
	hash := sha256.Sum256([]byte(token))
	expiry := s.now().Add(MFAChallengeTTL).Truncate(time.Second)
	if _, err = tx.ExecContext(ctx, "INSERT INTO mfa_challenges(token_hash,user_id,csrf_token,auth_version,expires_at) VALUES(?,?,?,?,?)", hash[:], user, randomToken(), version, expiry.Unix()); err != nil {
		return LoginResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Challenge: token, ExpiresAt: expiry}, nil
}
func (s *Store) LoginChallenge(ctx context.Context, token string) (LoginChallenge, error) {
	hash := sha256.Sum256([]byte(token))
	var challenge LoginChallenge
	var expiry int64
	err := s.db.QueryRowContext(ctx, `SELECT c.csrf_token,c.expires_at FROM mfa_challenges c JOIN users u ON u.id=c.user_id JOIN user_mfa m ON m.user_id=u.id WHERE c.token_hash=? AND c.expires_at>? AND c.attempts<5 AND c.auth_version=u.auth_version AND m.enabled=1`, hash[:], s.now().Unix()).Scan(&challenge.CSRF, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	challenge.ExpiresAt = time.Unix(expiry, 0)
	return challenge, err
}
func (s *Store) CompleteMFALogin(ctx context.Context, token, csrf string, factor MFAFactor) (LoginResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LoginResult{}, err
	}
	defer tx.Rollback()
	hash := sha256.Sum256([]byte(token))
	var user, expected string
	err = tx.QueryRowContext(ctx, `SELECT c.user_id,c.csrf_token FROM mfa_challenges c JOIN users u ON u.id=c.user_id JOIN user_mfa m ON m.user_id=u.id WHERE c.token_hash=? AND c.expires_at>? AND c.attempts<5 AND c.auth_version=u.auth_version AND m.enabled=1`, hash[:], s.now().Unix()).Scan(&user, &expected)
	if errors.Is(err, sql.ErrNoRows) {
		return LoginResult{}, ErrNotFound
	}
	if err != nil {
		return LoginResult{}, err
	}
	if !equalDigest([]byte(csrf), []byte(expected)) {
		return LoginResult{}, ErrMFACSRF
	}
	r, err := readMFATx(ctx, tx, user)
	if err != nil {
		return LoginResult{}, err
	}
	if err = s.factorProofTx(ctx, tx, r, factor); err != nil {
		if errors.Is(err, ErrMFAProof) {
			if _, e := tx.ExecContext(ctx, "UPDATE mfa_challenges SET attempts=attempts+1 WHERE token_hash=?", hash[:]); e != nil {
				return LoginResult{}, e
			}
		}
		return LoginResult{}, commitMFAFailure(tx, err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM mfa_challenges WHERE token_hash=?", hash[:]); err != nil {
		return LoginResult{}, err
	}
	full, session, err := s.newSessionTx(ctx, tx, user)
	if err != nil {
		return LoginResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: full, Session: session, ExpiresAt: session.ExpiresAt}, nil
}

// factorProofTx persists a shared failure budget and consumes exactly one proof.
// Callers commit proof/rate failures, but roll back unrelated/storage failures.
func (s *Store) factorProofTx(ctx context.Context, tx *sql.Tx, r mfaRow, factor MFAFactor) error {
	now := s.now().Unix()
	if r.failureSince == 0 || now-r.failureSince >= 300 {
		r.failures = 0
		r.failureSince = now
		if _, err := tx.ExecContext(ctx, "UPDATE user_mfa SET failures=0,failure_since=? WHERE user_id=?", now, r.user); err != nil {
			return err
		}
	}
	if r.failures >= 5 {
		return ErrMFARate
	}
	valid := false
	if factor.Recovery {
		if value := normalizeRecovery(factor.Code); value != "" {
			hash := sha256.Sum256([]byte(value))
			result, err := tx.ExecContext(ctx, "DELETE FROM mfa_recovery WHERE user_id=? AND code_hash=?", r.user, hash[:])
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			valid = n == 1
		}
	} else {
		secret, err := s.mfaSecret(r)
		if err != nil {
			return err
		}
		defer clear(secret)
		step := matchedTOTPStep(strings.TrimSpace(factor.Code), string(secret), s.now())
		if step >= 0 && step > r.lastStep {
			result, err := tx.ExecContext(ctx, "UPDATE user_mfa SET last_step=? WHERE user_id=? AND last_step<?", step, r.user, step)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			valid = n == 1
		}
	}
	if !valid {
		_, err := tx.ExecContext(ctx, "UPDATE user_mfa SET failures=failures+1 WHERE user_id=?", r.user)
		if err != nil {
			return err
		}
		return ErrMFAProof
	}
	_, err := tx.ExecContext(ctx, "UPDATE user_mfa SET failures=0,failure_since=0 WHERE user_id=?", r.user)
	return err
}
func matchedTOTPStep(code, secret string, now time.Time) int64 {
	step := now.Unix() / 30
	for _, candidate := range []int64{step, step - 1, step + 1} {
		if candidate < 0 {
			continue
		}
		valid, err := totp.ValidateCustom(code, secret, time.Unix(candidate*30, 0), totp.ValidateOpts{Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && valid {
			return candidate
		}
	}
	return -1
}
func commitMFAFailure(tx *sql.Tx, err error) error {
	if errors.Is(err, ErrMFAProof) || errors.Is(err, ErrMFARate) {
		if e := tx.Commit(); e != nil {
			return e
		}
	}
	return err
}
