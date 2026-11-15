// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// CachedAttachment is one attachment reference from cached ordinary mail. It is
// metadata only: bytes stay in the cached original MIME and are read through the
// existing message reader with its local preview isolation. No attachment bytes
// are duplicated or uploaded.
type CachedAttachment struct {
	AccountID  string
	Mailbox    string
	UID        uint32
	Validity   uint32
	Revision   int64
	Received   time.Time
	Subject    string
	Sender     string
	SenderName string
	Name       string
	Type       string
}

const (
	maxAttachmentMessages = 500
	maxAttachmentResults  = 1000
	maxAttachmentAccounts = 64
)

// AttachmentList enumerates attachment references across the caller-authorized
// cached accounts, newest message first. Encrypted mail carries no parsed
// attachments and is never listed; client-decrypted attachment names/bytes stay
// local to the browser.
func (s *Service) AttachmentList(ctx context.Context, accountIDs []string, limit int) ([]CachedAttachment, error) {
	result := []CachedAttachment{}
	if limit <= 0 || limit > maxAttachmentResults {
		limit = maxAttachmentResults
	}
	unique := make([]string, 0, maxAttachmentAccounts)
	seen := map[string]bool{}
	for _, id := range accountIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
		if len(unique) == maxAttachmentAccounts {
			break
		}
	}
	if len(unique) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(unique))
	args := make([]any, 0, len(unique)+1)
	for i, id := range unique {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, maxAttachmentMessages)
	rows, err := s.db.QueryContext(ctx, `SELECT m.account_id,m.mailbox,m.uid,b.uid_validity,c.revision,m.received_at,coalesce(json_extract(m.preview,'$.Subject'),''),coalesce(json_extract(m.preview,'$.Sender'),''),coalesce(json_extract(m.preview,'$.Name'),''),coalesce(json_extract(m.preview,'$.Attachments'),'[]') FROM mail_messages m JOIN mailboxes b ON b.account_id=m.account_id AND b.name=m.mailbox JOIN mail_connections c ON c.account_id=m.account_id WHERE m.account_id IN (`+strings.Join(placeholders, ",")+`) AND json_array_length(coalesce(json_extract(m.preview,'$.Attachments'),'[]'))>0 ORDER BY m.received_at DESC,m.uid DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item CachedAttachment
		var at int64
		var encoded string
		if err = rows.Scan(&item.AccountID, &item.Mailbox, &item.UID, &item.Validity, &item.Revision, &at, &item.Subject, &item.Sender, &item.SenderName, &encoded); err != nil {
			return nil, err
		}
		item.Received = time.Unix(at, 0)
		var attachments []Attachment
		if json.Unmarshal([]byte(encoded), &attachments) != nil {
			continue
		}
		for _, attachment := range attachments {
			if len(result) == limit {
				return result, nil
			}
			entry := item
			entry.Name = attachment.Name
			entry.Type = attachment.Type
			result = append(result, entry)
		}
	}
	return result, rows.Err()
}
