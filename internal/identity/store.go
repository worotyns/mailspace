// SPDX-License-Identifier: AGPL-3.0-only
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/alexedwards/argon2id"
	_ "modernc.org/sqlite"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrCredentials = errors.New("email or password not recognized")
	ErrNotFound    = errors.New("resource not found")
)

const SessionTTL = 8 * time.Hour

var passwordParams = &argon2id.Params{Memory: 64 * 1024, Iterations: 3, Parallelism: 2, SaltLength: 16, KeyLength: 32}

//go:embed migrations/001_init.sql
var migrationOne string

//go:embed migrations/002_mail_connections.sql
var migrationTwo string

//go:embed migrations/003_mail_cache.sql
var migrationThree string

//go:embed migrations/004_outbox.sql
var migrationFour string

//go:embed migrations/005_workspaces.sql
var migrationFive string

//go:embed migrations/006_message_spaces.sql
var migrationSix string

type User struct {
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
	ID       string `json:"id"`
	Email    string `json:"email"`
}
type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Session struct {
	User      User
	CSRF      string
	ExpiresAt time.Time
}
type Store struct {
	db        *sql.DB
	dummyHash string
}

// Open accepts a filesystem path, never a caller-provided SQLite DSN.
func Open(path string) (*Store, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, err
	}
	if info, e := os.Lstat(absolute); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("database must not be a symlink")
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: absolute}
	params := url.Values{}
	params.Add("_pragma", "foreign_keys(1)")
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "busy_timeout(5000)")
	uri.RawQuery = params.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db}
	if err = store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("identity migration: %w", err)
	}
	// Unknown users take the same password verification path as known users.
	store.dummyHash, err = argon2id.CreateHash(randomToken(), passwordParams)
	if err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)"); err != nil {
		return err
	}
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(version),0) FROM schema_migrations").Scan(&version); err != nil {
		return err
	}
	if version > 6 {
		return errors.New("database is newer than this binary; use a compatible version or restore backup")
	}
	if version == 0 {
		if _, err = tx.ExecContext(ctx, migrationOne); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,applied_at) VALUES(1,?)", time.Now().Unix()); err != nil {
			return err
		}
	}
	if version < 2 {
		if _, err = tx.ExecContext(ctx, migrationTwo); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,applied_at) VALUES(2,?)", time.Now().Unix()); err != nil {
			return err
		}
	}
	if version < 3 {
		if _, err = tx.ExecContext(ctx, migrationThree); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,applied_at) VALUES(3,?)", time.Now().Unix()); err != nil {
			return err
		}
	}
	if version < 4 {
		if _, err = tx.ExecContext(ctx, migrationFour); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,applied_at) VALUES(4,?)", time.Now().Unix()); err != nil {
			return err
		}
	}
	if version < 5 {
		if _, err = tx.ExecContext(ctx, migrationFive); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,applied_at) VALUES(5,?)", time.Now().Unix()); err != nil {
			return err
		}
	}
	if version < 6 {
		if _, err = tx.ExecContext(ctx, migrationSix); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,applied_at) VALUES(6,?)", time.Now().Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func randomToken() string {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("secure randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(value[:])
}
func NormalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || len(email) > 254 || address.Address != email || address.Name != "" {
		return "", errors.New("invalid email address")
	}
	return email, nil
}
func (s *Store) CreateUser(ctx context.Context, email, password string) (User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return User{}, errors.New("password must contain at least 12 characters and at most 1024 bytes")
	}
	hash, err := argon2id.CreateHash(password, passwordParams)
	if err != nil {
		return User{}, err
	}
	user := User{ID: randomToken(), Email: email}
	_, err = s.db.ExecContext(ctx, "INSERT INTO users(id,email,password_hash,created_at) VALUES(?,?,?,?)", user.ID, user.Email, hash, time.Now().Unix())
	return user, err
}
func (s *Store) Authenticate(ctx context.Context, email, password string) (User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		email = ""
	}
	if len(password) > 1024 {
		return User{}, ErrCredentials
	}
	var user User
	var hash string
	err = s.db.QueryRowContext(ctx, "SELECT id,email,password_hash,display_name,timezone FROM users WHERE email=?", email).Scan(&user.ID, &user.Email, &hash, &user.Name, &user.Timezone)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}
	exists := err == nil
	if !exists {
		hash = s.dummyHash
	}
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return User{}, err
	}
	if !match || !exists {
		return User{}, ErrCredentials
	}
	return user, nil
}
func (s *Store) NewSession(ctx context.Context, userID string) (string, Session, error) {
	token := randomToken()
	hash := sha256.Sum256([]byte(token))
	session := Session{CSRF: randomToken(), ExpiresAt: time.Now().Add(SessionTTL).Truncate(time.Second)}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", Session{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return "", Session{}, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT id,email,display_name,timezone FROM users WHERE id=?", userID).Scan(&session.User.ID, &session.User.Email, &session.User.Name, &session.User.Timezone); err != nil {
		return "", Session{}, ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES(?,?,?,?)", hash[:], userID, session.CSRF, session.ExpiresAt.Unix()); err != nil {
		return "", Session{}, err
	}
	if err = tx.Commit(); err != nil {
		return "", Session{}, err
	}
	return token, session, nil
}
func (s *Store) Session(ctx context.Context, token string) (Session, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return Session{}, ErrNotFound
	}
	hash := sha256.Sum256([]byte(token))
	var session Session
	var expiry int64
	err = s.db.QueryRowContext(ctx, "SELECT users.id,users.email,sessions.csrf_token,sessions.expires_at,users.display_name,users.timezone FROM sessions JOIN users ON users.id=sessions.user_id WHERE token_hash=? AND expires_at>?", hash[:], time.Now().Unix()).Scan(&session.User.ID, &session.User.Email, &session.CSRF, &expiry, &session.User.Name, &session.User.Timezone)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	session.ExpiresAt = time.Unix(expiry, 0)
	return session, err
}
func (s *Store) RevokeSession(ctx context.Context, token string) error {
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", hash[:])
	return err
}
func (s *Store) RevokeAll(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=?", userID)
	return err
}

// Accounts contain ownership metadata only at this stage, never mail credentials.
func (s *Store) CreateAccount(ctx context.Context, ownerID, name string) (Account, error) {
	if strings.TrimSpace(name) == "" || len(name) > 120 {
		return Account{}, errors.New("account name required (max 120 bytes)")
	}
	account := Account{ID: randomToken(), Name: strings.TrimSpace(name)}
	_, err := s.db.ExecContext(ctx, "INSERT INTO accounts(id,owner_id,name,created_at) VALUES(?,?,?,?)", account.ID, ownerID, account.Name, time.Now().Unix())
	return account, err
}
func (s *Store) Account(ctx context.Context, ownerID, id string) (Account, error) {
	var account Account
	err := s.db.QueryRowContext(ctx, "SELECT id,name FROM accounts WHERE id=? AND owner_id=?", id, ownerID).Scan(&account.ID, &account.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return account, err
}
func (s *Store) Accounts(ctx context.Context, ownerID string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name FROM accounts WHERE owner_id=? ORDER BY created_at,id", ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []Account{}
	for rows.Next() {
		var account Account
		if err = rows.Scan(&account.ID, &account.Name); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

// Database is for internal composition-root wiring of reviewed SQL repositories.
// HTTP handlers and external integrations must not receive this connection.
func (s *Store) Database() *sql.DB { return s.db }
