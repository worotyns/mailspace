// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/mailnet"
	"mailspace.local/mailspace/internal/mailsync"
	"regexp"
	"time"
)

var ErrNotFound = errors.New("outgoing message not found")
var ErrConflict = errors.New("send request was already used for another message")
var ErrCapacity = errors.New("outbox capacity reached")
var noncePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{26,64}$`)

type Record struct {
	ID        string
	AccountID string
	Subject   string
	State     string
	CopyState string
	CreatedAt time.Time
}
type Service struct {
	db       *sql.DB
	accounts *accounts.Service
	mail     *mailsync.Service
	sender   mailnet.Sender
}

func New(db *sql.DB, a *accounts.Service, m *mailsync.Service, s mailnet.Sender) *Service {
	return &Service{db: db, accounts: a, mail: m, sender: s}
}

// Recover never resends interrupted operations. A crash after DATA can mean the
// server accepted mail even if no local acceptance record was committed.
func (s *Service) Recover(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE outbox SET state=CASE WHEN state='sending' THEN 'unknown' ELSE state END,copy_state=CASE WHEN copy_state='copying' THEN 'unknown' ELSE copy_state END WHERE state='sending' OR copy_state='copying'`)
	return err
}
func (s *Service) Parent(ctx context.Context, owner, account, folder string, validity, uid uint32) (*mailsync.Preview, error) {
	messages, err := s.mail.Messages(ctx, owner, account, folder)
	if err != nil {
		return nil, err
	}
	for _, m := range messages {
		if m.Validity == validity && m.UID == uid {
			if m.Preview.Status == "pending" || m.Preview.Status == "too-large" {
				return nil, ErrInput
			}
			raw, err := s.mail.Raw(ctx, owner, account, folder, validity, uid)
			if err != nil {
				return nil, err
			}
			p := mailsync.Parse(raw)
			return &p, nil
		}
	}
	return nil, ErrNotFound
}
func (s *Service) Queue(ctx context.Context, owner, account, nonce string, draft Draft) (Record, error) {
	if !noncePattern.MatchString(nonce) {
		return Record{}, ErrInput
	}
	config, err := s.accounts.Get(ctx, owner, account)
	if err != nil {
		return Record{}, err
	}
	if draft.SourceRevision != 0 && draft.SourceRevision != config.Revision {
		return Record{}, ErrConflict
	}
	var parent *mailsync.Preview
	if draft.ReplyOutgoing != "" {
		if draft.ReplyUID != 0 || draft.ReplyValidity != 0 || draft.ReplyFolder != "" {
			return Record{}, ErrInput
		}
		source, err := s.RawSource(ctx, owner, account, draft.ReplyOutgoing)
		if err != nil {
			return Record{}, err
		}
		if source.Revision != config.Revision {
			return Record{}, ErrConflict
		}
		preview := mailsync.Parse(source.Raw)
		parent = &preview
	}
	if draft.ReplyUID != 0 || draft.ReplyValidity != 0 || draft.ReplyFolder != "" {
		parent, err = s.Parent(ctx, owner, account, draft.ReplyFolder, draft.ReplyValidity, draft.ReplyUID)
		if err != nil {
			return Record{}, err
		}
	}
	raw, to, err := Build(config.Address, draft, parent)
	if err != nil {
		return Record{}, err
	}
	encoded, _ := json.Marshal(draft)
	digest := sha256.Sum256(encoded)
	clear(encoded)
	recipients, _ := json.Marshal(to)
	id := rand.Text()
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM mail_connections JOIN accounts ON accounts.id=account_id WHERE account_id=? AND owner_id=?", account, owner).Scan(&revision); err != nil {
		return Record{}, ErrNotFound
	}
	if revision != config.Revision {
		return Record{}, ErrConflict
	}
	var existingID string
	var hash []byte
	err = tx.QueryRowContext(ctx, "SELECT id,input_hash FROM outbox WHERE account_id=? AND nonce=?", account, nonce).Scan(&existingID, &hash)
	if err == nil {
		if !bytes.Equal(hash, digest[:]) {
			return Record{}, ErrConflict
		}
		tx.Rollback()
		return s.Get(ctx, owner, existingID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Record{}, err
	}
	var total, count int64
	if err = tx.QueryRowContext(ctx, "SELECT coalesce(sum(length(raw)),0),count(*) FROM outbox WHERE account_id=?", account).Scan(&total, &count); err != nil {
		return Record{}, err
	}
	if total+int64(len(raw)) > 64<<20 || count >= 500 {
		return Record{}, ErrCapacity
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,account_id,nonce,input_hash,revision,recipients,raw,subject,state,copy_state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,'pending','not-attempted',?,?)`, id, account, nonce, digest[:], revision, string(recipients), raw, draft.Subject, now, now)
	if err != nil {
		return Record{}, err
	}
	if err = tx.Commit(); err != nil {
		return Record{}, err
	}
	return s.Get(ctx, owner, id)
}
func (s *Service) Get(ctx context.Context, owner, id string) (Record, error) {
	var r Record
	var at int64
	err := s.db.QueryRowContext(ctx, "SELECT outbox.id,account_id,subject,state,copy_state,outbox.created_at FROM outbox JOIN accounts ON accounts.id=account_id WHERE outbox.id=? AND owner_id=?", id, owner).Scan(&r.ID, &r.AccountID, &r.Subject, &r.State, &r.CopyState, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	r.CreatedAt = time.Unix(at, 0)
	return r, err
}
func (s *Service) List(ctx context.Context, owner, account string) ([]Record, error) {
	if _, err := s.accounts.Get(ctx, owner, account); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT outbox.id,subject,state,copy_state,outbox.created_at FROM outbox JOIN accounts ON accounts.id=account_id WHERE account_id=? AND owner_id=? ORDER BY outbox.created_at DESC,outbox.id DESC LIMIT 100", account, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Record{}
	for rows.Next() {
		var r Record
		var at int64
		r.AccountID = account
		if err = rows.Scan(&r.ID, &r.Subject, &r.State, &r.CopyState, &at); err != nil {
			return nil, err
		}
		r.CreatedAt = time.Unix(at, 0)
		result = append(result, r)
	}
	return result, rows.Err()
}

// Dispatch claims pending→sending once, before network work. Repeated POSTs and
// unknown/rejected/accepted states cannot trigger another SMTP attempt.
func (s *Service) Dispatch(ctx context.Context, owner, id string) error {
	record, err := s.Get(ctx, owner, id)
	if err != nil {
		return err
	}
	config, err := s.accounts.Get(ctx, owner, record.AccountID)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE outbox SET state='sending',updated_at=? WHERE id=? AND state='pending' AND revision=? AND account_id IN (SELECT id FROM accounts WHERE owner_id=?)`, time.Now().Unix(), id, config.Revision, owner)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		_, err = s.db.ExecContext(ctx, "UPDATE outbox SET state='rejected',updated_at=? WHERE id=? AND state='pending' AND revision!=? AND account_id IN (SELECT id FROM accounts WHERE owner_id=?)", time.Now().Unix(), id, config.Revision, owner)
		return err
	}
	settled := false
	defer func() {
		if !settled {
			durable, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_, _ = s.db.ExecContext(durable, "UPDATE outbox SET state='unknown' WHERE id=? AND state='sending'", id)
		}
	}()
	var raw []byte
	var encoded string
	var revision int64
	if err = s.db.QueryRowContext(ctx, "SELECT raw,recipients,revision FROM outbox WHERE id=?", id).Scan(&raw, &encoded, &revision); err != nil {
		return err
	}
	var recipients []string
	if json.Unmarshal([]byte(encoded), &recipients) != nil {
		return ErrInput
	}
	outcome := mailnet.Rejected
	err = s.accounts.WithSMTP(ctx, owner, record.AccountID, func(ep mailnet.Endpoint, login mailnet.Login, current int64) error {
		if current != revision {
			return ErrConflict
		}
		outcome = s.sender.Send(ctx, ep, login, config.Address, recipients, raw)
		return nil
	})
	if err != nil {
		outcome = mailnet.Rejected
	}
	if outcome != mailnet.Accepted && outcome != mailnet.Rejected {
		outcome = mailnet.Unknown
	}
	// Persist the result even if the browser disconnected after DATA.
	durable, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err = s.db.ExecContext(durable, "UPDATE outbox SET state=?,updated_at=? WHERE id=? AND state='sending'", string(outcome), time.Now().Unix(), id); err != nil {
		return err
	}
	settled = true
	if outcome != mailnet.Accepted {
		return nil
	}
	return s.copySent(context.WithoutCancel(ctx), owner, id, record.AccountID, revision, config.SentMailbox, raw)
}
func (s *Service) copySent(ctx context.Context, owner, id, account string, revision int64, folder string, raw []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if folder == "" {
		folder = "Sent"
	}
	result, err := s.db.ExecContext(ctx, "UPDATE outbox SET copy_state='copying' WHERE id=? AND state='accepted' AND copy_state='not-attempted'", id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return nil
	}
	err = s.accounts.WithIMAP(ctx, owner, account, func(ep mailnet.Endpoint, login mailnet.Login, current int64) error {
		if current != revision {
			return ErrConflict
		}
		return s.sender.AppendSent(ctx, ep, login, folder, raw)
	})
	state := "saved"
	if err != nil {
		state = "unknown"
	}
	durable, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	_, err = s.db.ExecContext(durable, "UPDATE outbox SET copy_state=?,updated_at=? WHERE id=? AND copy_state='copying'", state, time.Now().Unix(), id)
	return err
}
