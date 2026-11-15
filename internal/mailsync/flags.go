// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/mailnet"
)

// ErrFlagWritesDisabled means this installation did not enable the bounded
// provider flag capability. No remote or local change happens.
var ErrFlagWritesDisabled = errors.New("provider flag writes are disabled")

// SetSeen synchronizes the \Seen flag of one authorized cached message. The
// provider is contacted first with the caller's exact revision and UIDVALIDITY
// and must confirm the returned flags; only then is the local cache updated,
// inside a transaction that rechecks the connection revision. Other messages,
// mailboxes and accounts are never touched, and no local flag is flipped when
// the remote outcome is unknown.
func (s *Service) SetSeen(ctx context.Context, owner, id, folder string, validity, uid uint32, revision int64, seen bool) error {
	if s.writer == nil {
		return ErrFlagWritesDisabled
	}
	if err := s.authorized(ctx, owner, id); err != nil {
		return err
	}
	var cached int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM mail_messages m JOIN mailboxes b ON b.account_id=m.account_id AND b.name=m.mailbox JOIN mail_connections c ON c.account_id=m.account_id JOIN accounts a ON a.id=m.account_id WHERE a.owner_id=? AND a.id=? AND m.mailbox=? AND b.uid_validity=? AND m.uid=? AND c.revision=? AND m.raw IS NOT NULL`, owner, id, folder, validity, uid, revision).Scan(&cached)
	if err != nil {
		return err
	}
	if cached != 1 {
		return accounts.ErrNotFound
	}
	err = s.accounts.WithIMAP(ctx, owner, id, func(ep mailnet.Endpoint, login mailnet.Login, current int64) error {
		if current != revision {
			return ErrChanged
		}
		return s.writer.SetSeen(ctx, ep, login, folder, validity, uid, seen)
	})
	if err != nil {
		return err
	}
	return s.storeSeen(ctx, owner, id, folder, validity, uid, revision, seen)
}

// storeSeen records the confirmed provider flag locally. It only edits the
// flags column of one cached message and never rewrites message content.
func (s *Service) storeSeen(ctx context.Context, owner, id, folder string, validity, uid uint32, revision int64, seen bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.checkRevision(ctx, tx, owner, id, revision); err != nil {
		return err
	}
	var encoded string
	err = tx.QueryRowContext(ctx, `SELECT coalesce(m.flags,'[]') FROM mail_messages m JOIN mailboxes b ON b.account_id=m.account_id AND b.name=m.mailbox WHERE m.account_id=? AND m.mailbox=? AND b.uid_validity=? AND m.uid=?`, id, folder, validity, uid).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.ErrNotFound
	}
	if err != nil {
		return err
	}
	var flags []string
	if json.Unmarshal([]byte(encoded), &flags) != nil {
		return ErrChanged
	}
	updated := make([]string, 0, len(flags)+1)
	found := false
	for _, flag := range flags {
		if flag == `\Seen` {
			found = true
			if !seen {
				continue
			}
		}
		updated = append(updated, flag)
	}
	if seen && !found {
		updated = append(updated, `\Seen`)
	}
	value, _ := json.Marshal(updated)
	if _, err = tx.ExecContext(ctx, `UPDATE mail_messages SET flags=? WHERE account_id=? AND mailbox=? AND uid=?`, string(value), id, folder, uid); err != nil {
		return err
	}
	return tx.Commit()
}
