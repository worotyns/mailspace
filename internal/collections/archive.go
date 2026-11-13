// SPDX-License-Identifier: AGPL-3.0-only
package collections

import (
	"context"
	"mailspace.local/mailspace/internal/mail"
	"time"
)

func (s *Service) Archived(ctx context.Context, owner string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT x.message_key FROM message_archives x JOIN accounts a ON a.id=x.account_id JOIN mail_connections c ON c.account_id=a.id AND c.revision=x.account_revision WHERE a.owner_id=?`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, err
		}
		result[key] = true
	}
	return result, rows.Err()
}
func (s *Service) Archive(ctx context.Context, owner string, ref mail.SourceRef, archived bool) error {
	if s.resolver == nil {
		return ErrNotFound
	}
	identity, err := s.resolver.Identity(ctx, owner, ref)
	if err != nil {
		return ErrNotFound
	}
	if identity.AccountID != ref.AccountID || identity.Revision != ref.Revision {
		return ErrChanged
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var valid int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts a JOIN mail_connections c ON c.account_id=a.id WHERE a.id=? AND a.owner_id=? AND c.revision=?`, ref.AccountID, owner, ref.Revision).Scan(&valid); err != nil {
		return err
	}
	if valid != 1 {
		return ErrChanged
	}
	var result interface{ RowsAffected() (int64, error) }
	if archived {
		result, err = tx.ExecContext(ctx, `INSERT INTO message_archives(account_id,account_revision,message_key,created_at) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, ref.AccountID, ref.Revision, identity.Key, time.Now().Unix())
	} else {
		result, err = tx.ExecContext(ctx, `DELETE FROM message_archives WHERE account_id=? AND account_revision=? AND message_key=?`, ref.AccountID, ref.Revision, identity.Key)
	}
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO message_manual_revisions(account_id,account_revision,message_key,revision,updated_at) VALUES(?,?,?,1,?) ON CONFLICT(account_id,account_revision,message_key) DO UPDATE SET revision=revision+1,updated_at=excluded.updated_at`, ref.AccountID, ref.Revision, identity.Key, time.Now().Unix())
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
