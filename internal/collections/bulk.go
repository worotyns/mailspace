// SPDX-License-Identifier: AGPL-3.0-only
package collections

import (
	"context"
	"database/sql"
	"mailspace.local/mailspace/internal/mail"
	"time"
)

// Each bounded operation validates every owned current reference before writing.
// A failed/stale reference rolls back the entire local metadata transaction.
func (s *Service) mutateMany(ctx context.Context, owner string, refs []mail.SourceRef, mutate func(*sql.Tx, mail.MessageIdentity) (int64, error)) (int, error) {
	if s.resolver == nil || len(refs) == 0 || len(refs) > 500 {
		return 0, ErrInput
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	identities := make([]mail.MessageIdentity, len(refs))
	for i, ref := range refs {
		identity, err := s.resolver.Identity(ctx, owner, ref)
		if err != nil {
			return 0, ErrNotFound
		}
		if identity.AccountID != ref.AccountID || identity.Revision != ref.Revision || len(identity.Key) != 64 {
			return 0, ErrChanged
		}
		identities[i] = identity
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	changed := int64(0)
	seen := map[string]bool{}
	for i, identity := range identities {
		if seen[identity.Key] {
			continue
		}
		seen[identity.Key] = true
		ref := refs[i]
		var valid int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts a JOIN mail_connections c ON c.account_id=a.id WHERE a.id=? AND a.owner_id=? AND c.revision=?`, ref.AccountID, owner, ref.Revision).Scan(&valid); err != nil || valid != 1 {
			return 0, ErrChanged
		}
		if ref.Kind == "imap" {
			err = tx.QueryRowContext(ctx, `SELECT count(*) FROM mail_messages m JOIN mailboxes b ON b.account_id=m.account_id AND b.name=m.mailbox WHERE m.account_id=? AND m.mailbox=? AND m.uid=? AND b.uid_validity=?`, ref.AccountID, ref.Mailbox, ref.UID, ref.Validity).Scan(&valid)
		} else {
			err = tx.QueryRowContext(ctx, `SELECT count(*) FROM outbox WHERE account_id=? AND id=? AND revision=?`, ref.AccountID, ref.OutgoingID, ref.Revision).Scan(&valid)
		}
		if err != nil || valid != 1 {
			return 0, ErrChanged
		}
		n, e := mutate(tx, identity)
		if e != nil {
			return 0, e
		}
		changed += n
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return int(changed), nil
}
func (s *Service) ArchiveMany(ctx context.Context, owner string, refs []mail.SourceRef) (int, error) {
	return s.mutateMany(ctx, owner, refs, func(tx *sql.Tx, id mail.MessageIdentity) (int64, error) {
		result, err := tx.ExecContext(ctx, `INSERT INTO message_archives(account_id,account_revision,message_key,created_at) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, id.AccountID, id.Revision, id.Key, time.Now().Unix())
		if err != nil {
			return 0, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		if n > 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO message_manual_revisions(account_id,account_revision,message_key,revision,updated_at) VALUES(?,?,?,1,?) ON CONFLICT(account_id,account_revision,message_key) DO UPDATE SET revision=revision+1,updated_at=excluded.updated_at`, id.AccountID, id.Revision, id.Key, time.Now().Unix())
		}
		return n, err
	})
}
func (s *Service) LabelsMany(ctx context.Context, owner string, refs []mail.SourceRef, labels []string, add bool) (int, error) {
	if len(labels) == 0 || len(labels) > 64 {
		return 0, ErrInput
	}
	for _, id := range labels {
		if _, err := s.Get(ctx, owner, "label", id); err != nil {
			return 0, err
		}
	}
	count := -1
	return s.mutateMany(ctx, owner, refs, func(tx *sql.Tx, source mail.MessageIdentity) (int64, error) {
		total := int64(0)
		for _, id := range labels {
			var valid int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM message_collections WHERE id=? AND owner_id=? AND kind='label'", id, owner).Scan(&valid); err != nil || valid != 1 {
				return 0, ErrChanged
			}
			var result sql.Result
			var err error
			if add {
				if count < 0 {
					if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM message_collection_memberships m JOIN message_collections c ON c.id=m.collection_id WHERE c.owner_id=?`, owner).Scan(&count); err != nil {
						return 0, err
					}
				}
				var existing int
				if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM message_collection_memberships WHERE collection_id=? AND message_key=?`, id, source.Key).Scan(&existing); err != nil {
					return 0, err
				}
				if count >= 50000 && existing == 0 {
					return 0, ErrCapacity
				}
				result, err = tx.ExecContext(ctx, `INSERT INTO message_collection_memberships(collection_id,account_id,account_revision,message_key,source,actor_id,created_at) VALUES(?,?,?,?,'manual',?,?) ON CONFLICT DO NOTHING`, id, source.AccountID, source.Revision, source.Key, owner, time.Now().Unix())
			} else {
				result, err = tx.ExecContext(ctx, `DELETE FROM message_collection_memberships WHERE collection_id=? AND account_id=? AND account_revision=? AND message_key=?`, id, source.AccountID, source.Revision, source.Key)
			}
			if err != nil {
				return 0, err
			}
			n, e := result.RowsAffected()
			if e != nil {
				return 0, e
			}
			total += n
			if add {
				count += int(n)
			}
		}
		return total, nil // Status labels do not veto Space classification.
	})
}
