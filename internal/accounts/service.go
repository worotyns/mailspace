// SPDX-License-Identifier: AGPL-3.0-only
package accounts

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"mailspace.local/mailspace/internal/credentialvault"
	"mailspace.local/mailspace/internal/mailnet"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound    = errors.New("mail account not found")
	ErrInput       = errors.New("invalid mail account configuration")
	ErrCredentials = errors.New("stored mail credentials unavailable; restore the keyring backup")
)

type Checker interface {
	CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error
	CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error
}
type Input struct {
	Name        string
	SentMailbox string
	Address     string
	IMAP        mailnet.Endpoint
	SMTP        mailnet.Endpoint
	IMAPLogin   mailnet.Login `json:"-"`
	SMTPLogin   mailnet.Login `json:"-"`
}

func (Input) String() string   { return "<redacted mail account input>" }
func (Input) GoString() string { return "<redacted mail account input>" }

type Config struct {
	SentMailbox  string           `json:"sentMailbox"`
	Revision     int64            `json:"-"`
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Address      string           `json:"address"`
	IMAP         mailnet.Endpoint `json:"imap"`
	SMTP         mailnet.Endpoint `json:"smtp"`
	IMAPUsername string           `json:"imapUsername"`
	SMTPUsername string           `json:"smtpUsername"`
	CheckedAt    time.Time        `json:"checkedAt"`
}
type storedConfig struct {
	SentMailbox string
	Address     string
	IMAP        mailnet.Endpoint
	SMTP        mailnet.Endpoint
}
type record struct {
	id        string
	owner     string
	name      string
	config    string
	imap      credentialvault.Box
	smtp      credentialvault.Box
	checkedAt int64
	revision  int64
}
type Service struct {
	db      *sql.DB
	vault   *credentialvault.Vault
	checker Checker
}

func Open(db *sql.DB, keyDir string, checker Checker) (*Service, error) {
	var count int
	if err := db.QueryRow("SELECT count(*) FROM mail_connections").Scan(&count); err != nil {
		return nil, err
	}
	var vault *credentialvault.Vault
	var err error
	if count > 0 {
		vault, err = credentialvault.OpenExisting(keyDir)
	} else {
		vault, err = credentialvault.Open(keyDir)
	}
	if err != nil {
		return nil, err
	}
	return &Service{db: db, vault: vault, checker: checker}, nil
}
func validate(input Input) (Input, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.SentMailbox == "" {
		input.SentMailbox = "Sent"
	}
	if len(input.SentMailbox) > 1024 || strings.ContainsAny(input.SentMailbox, "\x00\r\n") {
		return Input{}, ErrInput
	}
	input.Address = strings.TrimSpace(input.Address)
	address, err := mail.ParseAddress(input.Address)
	if err != nil || address.Name != "" || address.Address != input.Address || len(input.Address) > 254 || input.Name == "" || len(input.Name) > 120 || strings.ContainsAny(input.Name, "\x00\r\n") {
		return Input{}, ErrInput
	}
	input.IMAP, err = mailnet.NormalizeEndpoint(input.IMAP)
	if err != nil {
		return Input{}, ErrInput
	}
	input.SMTP, err = mailnet.NormalizeEndpoint(input.SMTP)
	if err != nil {
		return Input{}, ErrInput
	}
	for _, login := range []mailnet.Login{input.IMAPLogin, input.SMTPLogin} {
		if login.Username == "" || len(login.Username) > 254 || login.Password == "" || len(login.Password) > 4096 || !utf8.ValidString(login.Username) || !utf8.ValidString(login.Password) || strings.ContainsAny(login.Username, "\x00\r\n") || strings.ContainsRune(login.Password, '\x00') {
			return Input{}, ErrInput
		}
	}
	return input, nil
}
func binding(owner, id, protocol string) []byte {
	value, _ := json.Marshal([]string{"mailspace-mail-login-v1", owner, id, protocol})
	return value
}
func (s *Service) seal(login mailnet.Login, owner, id, protocol string) (credentialvault.Box, error) {
	value, err := json.Marshal(struct {
		Username string
		Password string
	}{login.Username, login.Password})
	if err != nil {
		return credentialvault.Box{}, err
	}
	defer clear(value)
	return s.vault.Seal(value, binding(owner, id, protocol))
}
func (s *Service) unseal(box credentialvault.Box, owner, id, protocol string) (mailnet.Login, error) {
	value, err := s.vault.Unseal(box, binding(owner, id, protocol))
	if err != nil {
		return mailnet.Login{}, ErrCredentials
	}
	defer clear(value)
	var login struct {
		Username string
		Password string
	}
	if err = json.Unmarshal(value, &login); err != nil {
		return mailnet.Login{}, ErrCredentials
	}
	return mailnet.Login{Username: login.Username, Password: login.Password}, nil
}
func (s *Service) load(ctx context.Context, owner, id string) (record, error) {
	row := record{id: id, owner: owner}
	err := s.db.QueryRowContext(ctx, `SELECT accounts.name,mail_connections.config,imap_key_id,imap_secret,smtp_key_id,smtp_secret,checked_at,revision FROM mail_connections JOIN accounts ON accounts.id=mail_connections.account_id WHERE accounts.id=? AND accounts.owner_id=?`, id, owner).Scan(&row.name, &row.config, &row.imap.KeyID, &row.imap.Ciphertext, &row.smtp.KeyID, &row.smtp.Ciphertext, &row.checkedAt, &row.revision)
	if errors.Is(err, sql.ErrNoRows) {
		return record{}, ErrNotFound
	}
	return row, err
}
func (s *Service) Get(ctx context.Context, owner, id string) (Config, error) {
	row, err := s.load(ctx, owner, id)
	if err != nil {
		return Config{}, err
	}
	var stored storedConfig
	if err = json.Unmarshal([]byte(row.config), &stored); err != nil {
		return Config{}, ErrInput
	}
	imap, err := s.unseal(row.imap, owner, id, "imap")
	if err != nil {
		return Config{}, err
	}
	smtp, err := s.unseal(row.smtp, owner, id, "smtp")
	if err != nil {
		return Config{}, err
	}
	return Config{SentMailbox: stored.SentMailbox, Revision: row.revision, ID: id, Name: row.name, Address: stored.Address, IMAP: stored.IMAP, SMTP: stored.SMTP, IMAPUsername: imap.Username, SMTPUsername: smtp.Username, CheckedAt: time.Unix(row.checkedAt, 0)}, nil
}

// Save validates owner access before any remote operation. Both protocols must
// pass verified TLS authentication before a single database transaction commits.
func (s *Service) Save(ctx context.Context, owner, id string, input Input) (Config, error) {
	input, err := validate(input)
	if err != nil {
		return Config{}, err
	}
	exists := id != ""
	if exists {
		if _, err = s.load(ctx, owner, id); err != nil {
			return Config{}, err
		}
	} else {
		var userID string
		if err = s.db.QueryRowContext(ctx, "SELECT id FROM users WHERE id=?", owner).Scan(&userID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Config{}, ErrNotFound
			}
			return Config{}, err
		}
		id = rand.Text()
	}
	if s.checker == nil {
		return Config{}, errors.New("mail connector unavailable")
	}
	if err = s.checker.CheckIMAP(ctx, input.IMAP, input.IMAPLogin); err != nil {
		return Config{}, err
	}
	if err = s.checker.CheckSMTP(ctx, input.SMTP, input.SMTPLogin); err != nil {
		return Config{}, err
	}
	imap, err := s.seal(input.IMAPLogin, owner, id, "imap")
	if err != nil {
		return Config{}, err
	}
	smtp, err := s.seal(input.SMTPLogin, owner, id, "smtp")
	if err != nil {
		return Config{}, err
	}
	value, err := json.Marshal(storedConfig{SentMailbox: input.SentMailbox, Address: input.Address, IMAP: input.IMAP, SMTP: input.SMTP})
	if err != nil {
		return Config{}, err
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Config{}, err
	}
	defer tx.Rollback()
	if exists {
		result, err := tx.ExecContext(ctx, "UPDATE accounts SET name=? WHERE id=? AND owner_id=?", input.Name, id, owner)
		if err != nil {
			return Config{}, err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return Config{}, ErrNotFound
		}
	} else {
		if _, err = tx.ExecContext(ctx, "INSERT INTO accounts(id,owner_id,name,created_at) VALUES(?,?,?,?)", id, owner, input.Name, now); err != nil {
			return Config{}, err
		}
	}
	var revision int64
	err = tx.QueryRowContext(ctx, `INSERT INTO mail_connections(account_id,config,imap_key_id,imap_secret,smtp_key_id,smtp_secret,checked_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(account_id) DO UPDATE SET config=excluded.config,imap_key_id=excluded.imap_key_id,imap_secret=excluded.imap_secret,smtp_key_id=excluded.smtp_key_id,smtp_secret=excluded.smtp_secret,checked_at=excluded.checked_at,revision=mail_connections.revision+1 RETURNING revision`, id, string(value), imap.KeyID, imap.Ciphertext, smtp.KeyID, smtp.Ciphertext, now).Scan(&revision)
	if err != nil {
		return Config{}, err
	}
	if exists {
		if _, err = tx.ExecContext(ctx, "DELETE FROM mailboxes WHERE account_id=?", id); err != nil {
			return Config{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Config{}, err
	}
	return Config{Revision: revision, SentMailbox: input.SentMailbox, ID: id, Name: input.Name, Address: input.Address, IMAP: input.IMAP, SMTP: input.SMTP, IMAPUsername: input.IMAPLogin.Username, SMTPUsername: input.SMTPLogin.Username, CheckedAt: time.Unix(now, 0)}, nil
}
func (s *Service) Check(ctx context.Context, owner, id string) error {
	if s.checker == nil {
		return errors.New("mail connector unavailable")
	}
	row, err := s.load(ctx, owner, id)
	if err != nil {
		return err
	}
	var stored storedConfig
	if err = json.Unmarshal([]byte(row.config), &stored); err != nil {
		return ErrInput
	}
	imap, err := s.unseal(row.imap, owner, id, "imap")
	if err != nil {
		return err
	}
	smtp, err := s.unseal(row.smtp, owner, id, "smtp")
	if err != nil {
		return err
	}
	if err = s.checker.CheckIMAP(ctx, stored.IMAP, imap); err != nil {
		return err
	}
	return s.checker.CheckSMTP(ctx, stored.SMTP, smtp)
}

// RotateKey never deletes old key files. An interrupted database transaction is
// still readable because ciphertext carries its key id and every key is retained.
func (s *Service) RotateKey(ctx context.Context) (string, error) {
	id, err := s.vault.Rotate()
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT account_id,accounts.owner_id,imap_key_id,imap_secret,smtp_key_id,smtp_secret FROM mail_connections JOIN accounts ON accounts.id=mail_connections.account_id ORDER BY mail_connections.account_id`)
	if err != nil {
		return "", err
	}
	all := []record{}
	for rows.Next() {
		var row record
		if err = rows.Scan(&row.id, &row.owner, &row.imap.KeyID, &row.imap.Ciphertext, &row.smtp.KeyID, &row.smtp.Ciphertext); err != nil {
			rows.Close()
			return "", err
		}
		all = append(all, row)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return "", err
	}
	rows.Close()
	for _, row := range all {
		imapLogin, err := s.unseal(row.imap, row.owner, row.id, "imap")
		if err != nil {
			return "", err
		}
		smtpLogin, err := s.unseal(row.smtp, row.owner, row.id, "smtp")
		if err != nil {
			return "", err
		}
		imap, err := s.seal(imapLogin, row.owner, row.id, "imap")
		if err != nil {
			return "", err
		}
		smtp, err := s.seal(smtpLogin, row.owner, row.id, "smtp")
		if err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE mail_connections SET imap_key_id=?,imap_secret=?,smtp_key_id=?,smtp_secret=? WHERE account_id=?", imap.KeyID, imap.Ciphertext, smtp.KeyID, smtp.Ciphertext, row.id); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

// WithIMAP confines provider credentials to the transport operation after an
// owner-scoped lookup. Revision guards against writes from an obsolete config.
func (s *Service) WithIMAP(ctx context.Context, owner, id string, run func(mailnet.Endpoint, mailnet.Login, int64) error) error {
	row, err := s.load(ctx, owner, id)
	if err != nil {
		return err
	}
	var stored storedConfig
	if err = json.Unmarshal([]byte(row.config), &stored); err != nil {
		return ErrInput
	}
	login, err := s.unseal(row.imap, owner, id, "imap")
	if err != nil {
		return err
	}
	return run(stored.IMAP, login, row.revision)
}

// WithSMTP never exposes the login in HTTP responses or outbox records.
func (s *Service) WithSMTP(ctx context.Context, owner, id string, run func(mailnet.Endpoint, mailnet.Login, int64) error) error {
	row, err := s.load(ctx, owner, id)
	if err != nil {
		return err
	}
	var stored storedConfig
	if err = json.Unmarshal([]byte(row.config), &stored); err != nil {
		return ErrInput
	}
	login, err := s.unseal(row.smtp, owner, id, "smtp")
	if err != nil {
		return err
	}
	return run(stored.SMTP, login, row.revision)
}
