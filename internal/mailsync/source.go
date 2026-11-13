// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"time"
)

type Source struct {
	Raw      []byte
	At       time.Time
	Flags    []string
	Revision int64
}

func (Source) String() string   { return "<redacted IMAP source>" }
func (Source) GoString() string { return "<redacted IMAP source>" }

// Source binds a physical locator to the exact connection revision shown by the
// client. This avoids reusing an old UID when a mailbox account is reconfigured.
func (s *Service) Source(ctx context.Context, owner, id, folder string, validity, uid uint32, revision int64) (Source, error) {
	var result Source
	var at int64
	var flags string
	err := s.db.QueryRowContext(ctx, `SELECT m.raw,m.received_at,m.flags,c.revision FROM mail_messages m JOIN mailboxes b ON b.account_id=m.account_id AND b.name=m.mailbox JOIN accounts a ON a.id=m.account_id JOIN mail_connections c ON c.account_id=a.id WHERE a.owner_id=? AND a.id=? AND m.mailbox=? AND b.uid_validity=? AND m.uid=? AND c.revision=?`, owner, id, folder, validity, uid, revision).Scan(&result.Raw, &at, &flags, &result.Revision)
	if errors.Is(err, sql.ErrNoRows) || err == nil && result.Raw == nil {
		return result, accounts.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	if json.Unmarshal([]byte(flags), &result.Flags) != nil {
		return Source{}, ErrChanged
	}
	result.At = time.Unix(at, 0)
	return result, nil
}
