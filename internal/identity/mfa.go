// SPDX-License-Identifier: AGPL-3.0-only
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"mailspace.local/mailspace/internal/credentialvault"
)

var (
	ErrSecondFactor   = errors.New("a second authentication factor is required")
	ErrMFAProof       = errors.New("the authentication code was not accepted")
	ErrMFARate        = errors.New("too many authentication code attempts; wait five minutes")
	ErrMFACSRF        = errors.New("authentication challenge CSRF mismatch")
	ErrMFAUnavailable = errors.New("authentication factor secret unavailable; restore auth-keys or use an explicit recovery code")
	ErrMFAConflict    = errors.New("authenticator is already enabled")
)

const MFAChallengeTTL = 5 * time.Minute
const MFAEnrollmentTTL = 10 * time.Minute

type MFAEnrollment struct {
	Secret, URL string
	ExpiresAt   time.Time
}

func (MFAEnrollment) String() string   { return "<redacted authenticator enrollment>" }
func (MFAEnrollment) GoString() string { return "<redacted authenticator enrollment>" }

type MFAStatus struct {
	Enabled, Pending  bool
	RecoveryRemaining int
}
type MFAFactor struct {
	Code     string
	Recovery bool
}

func (MFAFactor) String() string   { return "<redacted authentication proof>" }
func (MFAFactor) GoString() string { return "<redacted authentication proof>" }

type RecoveryCodes []string

func (RecoveryCodes) String() string   { return "<redacted recovery codes>" }
func (RecoveryCodes) GoString() string { return "<redacted recovery codes>" }

type MFAResult struct {
	Codes   RecoveryCodes
	Token   string
	Session Session
}

func (MFAResult) String() string   { return "<redacted authentication result>" }
func (MFAResult) GoString() string { return "<redacted authentication result>" }

type mfaRow struct {
	user                                                         string
	box                                                          credentialvault.Box
	enabled                                                      bool
	setupSession                                                 []byte
	setupVersion, setupExpires, lastStep, failures, failureSince int64
}

func readMFATx(ctx context.Context, tx *sql.Tx, user string) (mfaRow, error) {
	r := mfaRow{user: user}
	err := tx.QueryRowContext(ctx, `SELECT key_id,secret,enabled,setup_session,setup_version,setup_expires,last_step,failures,failure_since FROM user_mfa WHERE user_id=?`, user).Scan(&r.box.KeyID, &r.box.Ciphertext, &r.enabled, &r.setupSession, &r.setupVersion, &r.setupExpires, &r.lastStep, &r.failures, &r.failureSince)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}
func (s *Store) MFAStatus(ctx context.Context, user string) (MFAStatus, error) {
	var status MFAStatus
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(m.enabled,0),coalesce(m.enabled=0 AND m.setup_expires>?,0),(SELECT count(*) FROM mfa_recovery WHERE user_id=u.id) FROM users u LEFT JOIN user_mfa m ON m.user_id=u.id WHERE u.id=?`, s.now().Unix(), user).Scan(&status.Enabled, &status.Pending, &status.RecoveryRemaining)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return status, err
}
func mfaAAD(user string) []byte { return []byte("mailspace:authenticator:v1:" + user) }
func (s *Store) mfaSecret(r mfaRow) ([]byte, error) {
	v, err := credentialvault.OpenExisting(s.authDir)
	if err != nil {
		return nil, ErrMFAUnavailable
	}
	secret, err := v.Unseal(r.box, mfaAAD(r.user))
	if err != nil {
		return nil, ErrMFAUnavailable
	}
	return secret, nil
}
func (s *Store) securityPassword(ctx context.Context, user, token, password string) (User, int64, error) {
	session, err := s.Session(ctx, token)
	if err != nil || session.User.ID != user {
		return User{}, 0, ErrCredentials
	}
	u, version, err := s.passwordProof(ctx, session.User.Email, password)
	if err != nil || u.ID != user {
		if err != nil && !errors.Is(err, ErrCredentials) {
			return User{}, 0, err
		}
		return User{}, 0, ErrCredentials
	}
	return u, version, nil
}
func (s *Store) securityCurrentTx(ctx context.Context, tx *sql.Tx, user, token string, version int64) error {
	hash := sha256.Sum256([]byte(token))
	var current int64
	err := tx.QueryRowContext(ctx, `SELECT u.auth_version FROM users u JOIN sessions s ON s.user_id=u.id WHERE u.id=? AND s.token_hash=? AND s.expires_at>?`, user, hash[:], s.now().Unix()).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) || err == nil && current != version {
		return ErrCredentials
	}
	return err
}
func (s *Store) BeginMFAEnrollment(ctx context.Context, user, token, password string) (MFAEnrollment, error) {
	u, version, err := s.securityPassword(ctx, user, token, password)
	if err != nil {
		return MFAEnrollment{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MFAEnrollment{}, err
	}
	defer tx.Rollback()
	if err = s.securityCurrentTx(ctx, tx, user, token, version); err != nil {
		return MFAEnrollment{}, err
	}
	r, err := readMFATx(ctx, tx, user)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return MFAEnrollment{}, err
	}
	if r.enabled {
		return MFAEnrollment{}, ErrMFAConflict
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Mailspace", AccountName: u.Email, Period: 30, SecretSize: 32, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return MFAEnrollment{}, err
	}
	var records int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM user_mfa").Scan(&records); err != nil {
		return MFAEnrollment{}, err
	}
	var v *credentialvault.Vault
	if records == 0 {
		v, err = credentialvault.Open(s.authDir)
	} else {
		v, err = credentialvault.OpenExisting(s.authDir)
	}
	if err != nil {
		return MFAEnrollment{}, ErrMFAUnavailable
	}
	secret := []byte(key.Secret())
	defer clear(secret)
	box, err := v.Seal(secret, mfaAAD(user))
	if err != nil {
		return MFAEnrollment{}, ErrMFAUnavailable
	}
	hash := sha256.Sum256([]byte(token))
	expiry := s.now().Add(MFAEnrollmentTTL).Truncate(time.Second)
	_, err = tx.ExecContext(ctx, `INSERT INTO user_mfa(user_id,key_id,secret,setup_session,setup_version,setup_expires) VALUES(?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET key_id=excluded.key_id,secret=excluded.secret,setup_session=excluded.setup_session,setup_version=excluded.setup_version,setup_expires=excluded.setup_expires,last_step=-1`, user, box.KeyID, box.Ciphertext, hash[:], version, expiry.Unix())
	if err != nil {
		return MFAEnrollment{}, err
	}
	if err = tx.Commit(); err != nil {
		return MFAEnrollment{}, err
	}
	return MFAEnrollment{Secret: key.Secret(), URL: key.URL(), ExpiresAt: expiry}, nil
}
func (s *Store) PendingMFAEnrollment(ctx context.Context, user, token string) (MFAEnrollment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MFAEnrollment{}, err
	}
	defer tx.Rollback()
	r, err := readMFATx(ctx, tx, user)
	if err != nil {
		return MFAEnrollment{}, err
	}
	if r.enabled || r.setupExpires <= s.now().Unix() {
		return MFAEnrollment{}, ErrNotFound
	}
	hash := sha256.Sum256([]byte(token))
	if !equalDigest(r.setupSession, hash[:]) {
		return MFAEnrollment{}, ErrNotFound
	}
	if err = s.securityCurrentTx(ctx, tx, user, token, r.setupVersion); err != nil {
		return MFAEnrollment{}, err
	}
	var email string
	if err = tx.QueryRowContext(ctx, "SELECT email FROM users WHERE id=?", user).Scan(&email); err != nil {
		return MFAEnrollment{}, err
	}
	secret, err := s.mfaSecret(r)
	if err != nil {
		return MFAEnrollment{}, err
	}
	defer clear(secret)
	query := url.Values{"secret": {string(secret)}, "issuer": {"Mailspace"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	uri := url.URL{Scheme: "otpauth", Host: "totp", Path: "Mailspace:" + email, RawQuery: query.Encode()}
	return MFAEnrollment{Secret: string(secret), URL: uri.String(), ExpiresAt: time.Unix(r.setupExpires, 0)}, nil
}
func (s *Store) ConfirmMFAEnrollment(ctx context.Context, user, token, code string) (MFAResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MFAResult{}, err
	}
	defer tx.Rollback()
	r, err := readMFATx(ctx, tx, user)
	if err != nil {
		return MFAResult{}, err
	}
	hash := sha256.Sum256([]byte(token))
	if r.enabled || r.setupExpires <= s.now().Unix() || !equalDigest(r.setupSession, hash[:]) {
		return MFAResult{}, ErrNotFound
	}
	if err = s.securityCurrentTx(ctx, tx, user, token, r.setupVersion); err != nil {
		return MFAResult{}, err
	}
	if err = s.factorProofTx(ctx, tx, r, MFAFactor{Code: code}); err != nil {
		return MFAResult{}, commitMFAFailure(tx, err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE user_mfa SET enabled=1,setup_session=NULL,setup_expires=0 WHERE user_id=?", user); err != nil {
		return MFAResult{}, err
	}
	codes, err := replaceRecoveryTx(ctx, tx, user)
	if err != nil {
		return MFAResult{}, err
	}
	return s.finishMFAChangeTx(ctx, tx, user, codes)
}
func (s *Store) DisableMFA(ctx context.Context, user, token, password string, factor MFAFactor) (MFAResult, error) {
	return s.changeMFA(ctx, user, token, password, factor, true)
}
func (s *Store) RegenerateRecoveryCodes(ctx context.Context, user, token, password string, factor MFAFactor) (MFAResult, error) {
	return s.changeMFA(ctx, user, token, password, factor, false)
}
func (s *Store) changeMFA(ctx context.Context, user, token, password string, factor MFAFactor, disable bool) (MFAResult, error) {
	_, version, err := s.securityPassword(ctx, user, token, password)
	if err != nil {
		return MFAResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MFAResult{}, err
	}
	defer tx.Rollback()
	if err = s.securityCurrentTx(ctx, tx, user, token, version); err != nil {
		return MFAResult{}, err
	}
	r, err := readMFATx(ctx, tx, user)
	if err != nil {
		return MFAResult{}, err
	}
	if !r.enabled {
		return MFAResult{}, ErrNotFound
	}
	if err = s.factorProofTx(ctx, tx, r, factor); err != nil {
		return MFAResult{}, commitMFAFailure(tx, err)
	}
	var codes RecoveryCodes
	if disable {
		_, err = tx.ExecContext(ctx, "DELETE FROM user_mfa WHERE user_id=?", user)
	} else {
		codes, err = replaceRecoveryTx(ctx, tx, user)
	}
	if err != nil {
		return MFAResult{}, err
	}
	return s.finishMFAChangeTx(ctx, tx, user, codes)
}
func (s *Store) finishMFAChangeTx(ctx context.Context, tx *sql.Tx, user string, codes RecoveryCodes) (MFAResult, error) {
	if err := s.revokeSessionsTx(ctx, tx, user); err != nil {
		return MFAResult{}, err
	}
	token, session, err := s.newSessionTx(ctx, tx, user)
	if err != nil {
		return MFAResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return MFAResult{}, err
	}
	return MFAResult{Codes: codes, Token: token, Session: session}, nil
}
func (s *Store) revokeSessionsTx(ctx context.Context, tx *sql.Tx, user string) error {
	result, err := tx.ExecContext(ctx, "UPDATE users SET auth_version=auth_version+1 WHERE id=?", user)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM mfa_challenges WHERE user_id=?", user); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=?", user)
	return err
}

// ResetMFA is an operator-only local recovery action, never an HTTP route.
func (s *Store) ResetMFA(ctx context.Context, email string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var user string
	err = tx.QueryRowContext(ctx, "SELECT id FROM users WHERE email=?", email).Scan(&user)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM user_mfa WHERE user_id=?", user); err != nil {
		return err
	}
	if err = s.revokeSessionsTx(ctx, tx, user); err != nil {
		return err
	}
	return tx.Commit()
}
func normalizeRecovery(value string) string {
	value = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	if len(value) != 32 {
		return ""
	}
	for _, r := range value {
		if !(r >= 'A' && r <= 'Z' || r >= '2' && r <= '7') {
			return ""
		}
	}
	return value
}
func replaceRecoveryTx(ctx context.Context, tx *sql.Tx, user string) (RecoveryCodes, error) {
	if _, err := tx.ExecContext(ctx, "DELETE FROM mfa_recovery WHERE user_id=?", user); err != nil {
		return nil, err
	}
	codes := make(RecoveryCodes, 0, 10)
	for range 10 {
		var raw [20]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, err
		}
		value := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])
		clear(raw[:])
		hash := sha256.Sum256([]byte(value))
		if _, err := tx.ExecContext(ctx, "INSERT INTO mfa_recovery(user_id,code_hash) VALUES(?,?)", user, hash[:]); err != nil {
			return nil, err
		}
		codes = append(codes, value[:8]+"-"+value[8:16]+"-"+value[16:24]+"-"+value[24:])
	}
	return codes, nil
}

// CancelMFAEnrollment removes only a pending setup owned by the current session.
func (s *Store) CancelMFAEnrollment(ctx context.Context, user, token string) error {
	hash := sha256.Sum256([]byte(token))
	result, err := s.db.ExecContext(ctx, `DELETE FROM user_mfa WHERE user_id=? AND enabled=0 AND setup_session=? AND EXISTS(SELECT 1 FROM sessions WHERE user_id=? AND token_hash=? AND expires_at>?)`, user, hash[:], user, hash[:], s.now().Unix())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrNotFound
	}
	return err
}
