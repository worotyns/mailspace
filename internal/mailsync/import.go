// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"context"
	"database/sql"
	"errors"
	"mailspace.local/mailspace/internal/mailnet"
	"time"
)

type ImportPolicy struct {
	Mode     string
	Revision int64
}

func (s *Service) ImportPolicy(ctx context.Context, owner, id string) (ImportPolicy, error) {
	if err := s.authorized(ctx, owner, id); err != nil {
		return ImportPolicy{}, err
	}
	result := ImportPolicy{Mode: "recent"} // Preserve existing installations/fixtures.
	err := s.db.QueryRowContext(ctx, "SELECT mode,revision FROM mail_import_settings WHERE account_id=?", id).Scan(&result.Mode, &result.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	return result, err
}

// SetImportMode changes future body downloads, never deletes imported mail.
func (s *Service) SetImportMode(ctx context.Context, owner, id, mode string) error {
	if mode != "fresh" && mode != "recent" {
		return ErrChanged
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var valid int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM accounts JOIN mail_connections ON account_id=accounts.id WHERE accounts.id=? AND owner_id=?", id, owner).Scan(&valid); err != nil {
		return err
	}
	if valid != 1 {
		return ErrChanged
	}
	var previous string
	err = tx.QueryRowContext(ctx, "SELECT mode FROM mail_import_settings WHERE account_id=?", id).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previous == mode {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mail_import_settings(account_id,mode,revision,updated_at) VALUES(?,?,1,?) ON CONFLICT(account_id) DO UPDATE SET mode=excluded.mode,revision=revision+1,updated_at=excluded.updated_at`, id, mode, time.Now().Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM mail_import_baselines WHERE account_id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) importWindow(ctx context.Context, id string, accountRevision int64, box mailnet.Mailbox, known map[uint32]bool, policy ImportPolicy) (mailnet.Mailbox, error) {
	if policy.Mode != "fresh" {
		return box, nil
	}
	var baseline uint32
	err := s.db.QueryRowContext(ctx, "SELECT baseline_uid FROM mail_import_baselines WHERE account_id=? AND mailbox=? AND account_revision=? AND uid_validity=?", id, box.Name, accountRevision, box.UIDValidity).Scan(&baseline)
	if errors.Is(err, sql.ErrNoRows) {
		for _, msg := range box.Messages {
			if msg.UID > baseline {
				baseline = msg.UID
			}
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO mail_import_baselines(account_id,mailbox,account_revision,uid_validity,baseline_uid) VALUES(?,?,?,?,?) ON CONFLICT(account_id,mailbox) DO UPDATE SET account_revision=excluded.account_revision,uid_validity=excluded.uid_validity,baseline_uid=excluded.baseline_uid`, id, box.Name, accountRevision, box.UIDValidity, baseline)
	}
	if err != nil {
		return box, err
	}
	messages := make([]mailnet.RemoteMessage, 0, len(box.Messages))
	for _, msg := range box.Messages {
		if msg.UID > baseline || known[msg.UID] {
			messages = append(messages, msg)
		}
	}
	box.Messages = messages
	return box, nil
}
