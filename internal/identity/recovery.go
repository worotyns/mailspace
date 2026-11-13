// SPDX-License-Identifier: AGPL-3.0-only
package identity

import (
	"context"
	"database/sql"
	"errors"
	"github.com/alexedwards/argon2id"
	"unicode/utf8"
)

// ResetPassword is an operator-only local recovery operation. Password change
// and revocation commit together; it does not touch provider or OpenPGP keys.
func (s *Store) ResetPassword(ctx context.Context, email, password string) error {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return errors.New("password must contain at least 12 characters and at most 1024 bytes")
	}
	hash, err := argon2id.CreateHash(password, passwordParams)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE users SET password_hash=? WHERE email=?", hash, normalized)
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
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=(SELECT id FROM users WHERE email=?)", normalized); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) RevokeUserSessions(ctx context.Context, email string) error {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	var id string
	if err = s.db.QueryRowContext(ctx, "SELECT id FROM users WHERE email=?", normalized).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return s.RevokeAll(ctx, id)
}
