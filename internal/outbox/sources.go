// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"context"
	"database/sql"
	"errors"
)

type Source struct {
	Record   Record
	Raw      []byte
	Revision int64
}

func (Source) String() string   { return "<redacted outgoing source>" }
func (Source) GoString() string { return "<redacted outgoing source>" }

// Sources is owner/current-configuration scoped and never dispatches mail. It
// allows a sent/unknown attempt to appear in the dialog before IMAP catches up.
func (s *Service) Sources(ctx context.Context, owner, account string) ([]Source, error) {
	if _, err := s.accounts.Get(ctx, owner, account); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT o.id,o.raw,o.revision FROM outbox o JOIN accounts a ON a.id=o.account_id JOIN mail_connections c ON c.account_id=a.id WHERE a.owner_id=? AND a.id=? AND c.revision=o.revision ORDER BY o.created_at DESC,o.id DESC LIMIT 100`, owner, account)
	if err != nil {
		return nil, err
	}
	type entry struct {
		id       string
		raw      []byte
		revision int64
	}
	var entries []entry
	for rows.Next() {
		var value entry
		if err = rows.Scan(&value.id, &value.raw, &value.revision); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := []Source{}
	for _, e := range entries {
		record, err := s.Get(ctx, owner, e.id)
		if err != nil {
			return nil, err
		}
		result = append(result, Source{Record: record, Raw: e.raw, Revision: e.revision})
	}
	return result, nil
}
func (s *Service) RawSource(ctx context.Context, owner, account, id string) (Source, error) {
	var source Source
	err := s.db.QueryRowContext(ctx, `SELECT o.raw,o.revision FROM outbox o JOIN accounts a ON a.id=o.account_id JOIN mail_connections c ON c.account_id=a.id WHERE a.owner_id=? AND a.id=? AND o.id=? AND c.revision=o.revision`, owner, account, id).Scan(&source.Raw, &source.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return source, ErrNotFound
	}
	if err != nil {
		return source, err
	}
	source.Record, err = s.Get(ctx, owner, id)
	return source, err
}
