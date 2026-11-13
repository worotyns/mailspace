// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/mailnet"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrBusy = errors.New("mail sync is busy; retry shortly")
var ErrChanged = errors.New("mail account changed; retry synchronization")

const MaxAccountBytes = 64 << 20

type Service struct {
	db       *sql.DB
	accounts *accounts.Service
	reader   mailnet.Reader
	mu       sync.Mutex
	queue    chan syncRequest
}

func New(db *sql.DB, a *accounts.Service, r mailnet.Reader) *Service {
	return &Service{db: db, accounts: a, reader: r, queue: make(chan syncRequest, 32)}
}

type Folder struct {
	Name     string
	Total    int
	Cached   int
	Pending  int
	SyncedAt time.Time
}
type CachedMessage struct {
	AccountID string
	Mailbox   string
	Validity  uint32
	UID       uint32
	Flags     []string
	Received  time.Time
	Preview   Preview
}

func (s *Service) authorized(ctx context.Context, owner, id string) error {
	var found string
	err := s.db.QueryRowContext(ctx, "SELECT accounts.id FROM accounts JOIN mail_connections ON account_id=accounts.id WHERE owner_id=? AND accounts.id=?", owner, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.ErrNotFound
	}
	return err
}

// Sync is bounded and read-only remotely. Each complete mailbox snapshot commits
// atomically, while already downloaded MIME is reused by UID within UIDVALIDITY.
func (s *Service) Sync(ctx context.Context, owner, id string) error {
	if err := s.authorized(ctx, owner, id); err != nil {
		return err
	}
	if !s.mu.TryLock() {
		return ErrBusy
	}
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	return s.accounts.WithIMAP(ctx, owner, id, func(ep mailnet.Endpoint, login mailnet.Login, revision int64) error {
		remote, err := s.reader.OpenRead(ctx, ep, login)
		if err != nil {
			return err
		}
		defer remote.Close()
		names, err := remote.Mailboxes()
		if err != nil {
			return err
		}
		sort.Strings(names)
		remaining := 20
		bytesLeft := 16 << 20
		for _, name := range names {
			snapshot, err := remote.Snapshot(name)
			if err != nil {
				return err
			}
			known, used, err := s.known(ctx, owner, id, snapshot)
			if err != nil {
				return err
			}
			pending := map[uint32][]byte{}
			previews := map[uint32]Preview{}
			// Newest first. Next sync resumes the remaining UIDs from durable rows.
			sort.Slice(snapshot.Messages, func(i, j int) bool { return snapshot.Messages[i].UID > snapshot.Messages[j].UID })
			for _, msg := range snapshot.Messages {
				if known[msg.UID] {
					continue
				}
				if msg.Size > mailnet.MaxMIMEBytes {
					previews[msg.UID] = Preview{Subject: "Message exceeds preview limit", Name: "Unknown sender", Status: "too-large"}
					continue
				}
				if remaining == 0 || msg.Size > int64(bytesLeft) || used+msg.Size > MaxAccountBytes {
					continue
				}
				raw, err := remote.MIME(name, snapshot.UIDValidity, msg.UID)
				if err != nil {
					return err
				}
				if len(raw) > bytesLeft || used+int64(len(raw)) > MaxAccountBytes {
					continue
				}
				remaining--
				bytesLeft -= len(raw)
				used += int64(len(raw))
				pending[msg.UID] = raw
				previews[msg.UID] = Parse(raw)
			}
			if err = s.apply(ctx, owner, id, revision, snapshot, pending, previews); err != nil {
				return err
			}
		}
		// Delete disappeared folders only after every listed folder was read. Failure
		// never turns a partial LIST/snapshot into a destructive account-wide reset.
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = s.checkRevision(ctx, tx, owner, id, revision); err != nil {
			return err
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
		args := []any{id}
		for _, name := range names {
			args = append(args, name)
		}
		query := "DELETE FROM mailboxes WHERE account_id=?"
		if len(names) > 0 {
			query += " AND name NOT IN (" + placeholders + ")"
		}
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
		return tx.Commit()
	})
}
func (s *Service) known(ctx context.Context, owner, id string, box mailnet.Mailbox) (map[uint32]bool, int64, error) {
	known := map[uint32]bool{}
	if err := s.authorized(ctx, owner, id); err != nil {
		return nil, 0, err
	}
	var used int64
	if err := s.db.QueryRowContext(ctx, "SELECT coalesce(sum(length(raw)),0) FROM mail_messages WHERE account_id=?", id).Scan(&used); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT uid,preview FROM mail_messages JOIN mailboxes ON mailboxes.account_id=mail_messages.account_id AND name=mailbox WHERE mail_messages.account_id=? AND mailbox=? AND uid_validity=?", id, box.Name, box.UIDValidity)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid uint32
		var encoded string
		if err = rows.Scan(&uid, &encoded); err != nil {
			return nil, 0, err
		}
		var p Preview
		if json.Unmarshal([]byte(encoded), &p) != nil {
			return nil, 0, ErrChanged
		}
		known[uid] = p.Status != "pending"
	}
	return known, used, rows.Err()
}
func (s *Service) checkRevision(ctx context.Context, tx *sql.Tx, owner, id string, revision int64) error {
	var value int64
	if err := tx.QueryRowContext(ctx, "SELECT revision FROM mail_connections JOIN accounts ON accounts.id=account_id WHERE account_id=? AND owner_id=?", id, owner).Scan(&value); err != nil {
		return accounts.ErrNotFound
	}
	if value != revision {
		return ErrChanged
	}
	return nil
}
func (s *Service) apply(ctx context.Context, owner, id string, revision int64, box mailnet.Mailbox, raw map[uint32][]byte, previews map[uint32]Preview) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.checkRevision(ctx, tx, owner, id, revision); err != nil {
		return err
	}
	var validity uint32
	err = tx.QueryRowContext(ctx, "SELECT uid_validity FROM mailboxes WHERE account_id=? AND name=?", id, box.Name).Scan(&validity)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if validity != box.UIDValidity {
		if _, err = tx.ExecContext(ctx, "DELETE FROM mailboxes WHERE account_id=? AND name=?", id, box.Name); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mailboxes(account_id,name,uid_validity,total,synced_at) VALUES(?,?,?,?,?) ON CONFLICT(account_id,name) DO UPDATE SET total=excluded.total,synced_at=excluded.synced_at`, id, box.Name, box.UIDValidity, box.Total, time.Now().Unix()); err != nil {
		return err
	}
	args := []any{id, box.Name}
	where := ""
	if len(box.Messages) > 0 {
		where = " AND uid NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(box.Messages)), ",") + ")"
		for _, msg := range box.Messages {
			args = append(args, msg.UID)
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM mail_messages WHERE account_id=? AND mailbox=?"+where, args...); err != nil {
		return err
	}
	for _, msg := range box.Messages {
		p, exists := previews[msg.UID]
		if !exists {
			p = Preview{Subject: "Message awaiting download", Name: "Unknown sender", Status: "pending"}
		}
		preview, _ := json.Marshal(p)
		flags, _ := json.Marshal(msg.Flags)
		_, err = tx.ExecContext(ctx, `INSERT INTO mail_messages(account_id,mailbox,uid,flags,size,received_at,raw,preview) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(account_id,mailbox,uid) DO UPDATE SET flags=excluded.flags,size=excluded.size,received_at=excluded.received_at,raw=coalesce(excluded.raw,mail_messages.raw),preview=CASE WHEN ? THEN excluded.preview ELSE mail_messages.preview END`, id, box.Name, msg.UID, string(flags), msg.Size, msg.Received.Unix(), raw[msg.UID], string(preview), exists)
		if err != nil {
			return err
		}
	}
	var used int64
	if err = tx.QueryRowContext(ctx, "SELECT coalesce(sum(length(raw)),0) FROM mail_messages WHERE account_id=?", id).Scan(&used); err != nil {
		return err
	}
	if used > MaxAccountBytes {
		return mailnet.ErrMailLimit
	}
	return tx.Commit()
}
func (s *Service) Folders(ctx context.Context, owner, id string) ([]Folder, error) {
	if err := s.authorized(ctx, owner, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name,total,synced_at,count(uid),coalesce(sum(CASE WHEN json_extract(preview,'$.Status')='pending' THEN 1 ELSE 0 END),0) FROM mailboxes LEFT JOIN mail_messages ON mail_messages.account_id=mailboxes.account_id AND mailbox=name WHERE mailboxes.account_id=? GROUP BY name ORDER BY CASE WHEN name='INBOX' THEN 0 ELSE 1 END,name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Folder{}
	for rows.Next() {
		var f Folder
		var at int64
		if err = rows.Scan(&f.Name, &f.Total, &at, &f.Cached, &f.Pending); err != nil {
			return nil, err
		}
		f.SyncedAt = time.Unix(at, 0)
		result = append(result, f)
	}
	return result, rows.Err()
}
func (s *Service) Messages(ctx context.Context, owner, id, folder string) ([]CachedMessage, error) {
	if err := s.authorized(ctx, owner, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT mailbox,uid_validity,uid,flags,received_at,preview,CASE WHEN json_extract(preview,'$.ContentHash') IS NULL THEN raw ELSE NULL END FROM mail_messages JOIN mailboxes ON mailboxes.account_id=mail_messages.account_id AND name=mailbox WHERE mail_messages.account_id=? AND mailbox=? ORDER BY received_at,uid LIMIT 500`, id, folder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CachedMessage{}
	for rows.Next() {
		var m CachedMessage
		var flags, p string
		var at int64
		var legacyRaw []byte
		if err = rows.Scan(&m.Mailbox, &m.Validity, &m.UID, &flags, &at, &p, &legacyRaw); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(flags), &m.Flags) != nil || json.Unmarshal([]byte(p), &m.Preview) != nil {
			return nil, ErrChanged
		}
		if legacyRaw != nil {
			m.Preview = Parse(legacyRaw)
		}
		m.AccountID = id
		m.Received = time.Unix(at, 0)
		result = append(result, m)
	}
	return result, rows.Err()
}

// Raw returns original provider bytes only after owner and generation checks.
// It never accepts client-decrypted content or performs server decryption.
func (s *Service) Raw(ctx context.Context, owner, id, folder string, validity, uid uint32) ([]byte, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT raw FROM mail_messages JOIN mailboxes ON mailboxes.account_id=mail_messages.account_id AND mailboxes.name=mail_messages.mailbox JOIN accounts ON accounts.id=mail_messages.account_id WHERE owner_id=? AND accounts.id=? AND mailbox=? AND uid_validity=? AND uid=?`, owner, id, folder, validity, uid).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || err == nil && raw == nil {
		return nil, accounts.ErrNotFound
	}
	return raw, err
}
