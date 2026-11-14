// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"bytes"
	"context"
	"encoding/json"
	"net/mail"
)

const maxThreadHeaders = 64 << 10

// Restrict legacy repair reads to a complete header prefix where possible; the
// delimiter search itself is bounded to 64 KiB inside SQLite, never the body.
const threadingPrefixSQL = `substr(m.raw,1,min(coalesce(nullif(instr(substr(m.raw,1,65536),x'0d0a0d0a'),0)+3,65536),coalesce(nullif(instr(substr(m.raw,1,65536),x'0a0a'),0)+1,65536)))`

func combinedReferences(references, inReplyTo string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, header := range []string{references, inReplyTo} {
		for _, id := range ids(header) {
			if !seen[id] {
				seen[id] = true
				result = append(result, id)
			}
		}
	}
	return result // At most 64 parsed IDs per header, 128 combined.
}

// Header-only legacy repair never reads/decodes MIME bodies. Require a complete
// header terminator within the existing 64 KiB mail-header budget.
func repairedReferences(prefix []byte) ([]string, bool) {
	if len(prefix) > maxThreadHeaders {
		prefix = prefix[:maxThreadHeaders]
	}
	end, width := bytes.Index(prefix, []byte("\r\n\r\n")), 4
	if lf := bytes.Index(prefix, []byte("\n\n")); lf >= 0 && (end < 0 || lf < end) {
		end, width = lf, 2
	}
	if end < 0 {
		return nil, false
	}
	message, err := mail.ReadMessage(bytes.NewReader(prefix[:end+width]))
	if err != nil {
		return nil, false
	}
	return combinedReferences(message.Header.Get("References"), message.Header.Get("In-Reply-To")), true
}

type threadingRepair struct {
	uid               uint32
	validity          uint32
	revision          int64
	original, updated string
	prefix            []byte
}

// Persist after closing the read cursor. A concurrent source, account revision
// or preview change aborts the entire bounded repair instead of overwriting it.
func (s *Service) repairThreading(ctx context.Context, owner, account, folder string, repairs []threadingRepair) error {
	if len(repairs) == 0 {
		return nil
	}
	if len(repairs) > 500 {
		return ErrChanged
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, repair := range repairs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		result, err := tx.ExecContext(ctx, `UPDATE mail_messages SET preview=? WHERE account_id=? AND mailbox=? AND uid=? AND preview=? AND substr(raw,1,length(?))=? AND EXISTS(SELECT 1 FROM accounts a JOIN mail_connections c ON c.account_id=a.id WHERE a.id=? AND a.owner_id=? AND c.revision=?) AND EXISTS(SELECT 1 FROM mailboxes b WHERE b.account_id=? AND b.name=? AND b.uid_validity=?)`, repair.updated, account, folder, repair.uid, repair.original, repair.prefix, repair.prefix, account, owner, repair.revision, account, folder, repair.validity)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrChanged
		}
	}
	return tx.Commit()
}

func repairedPreview(preview Preview, prefix []byte) (Preview, string, bool) {
	refs, ok := repairedReferences(prefix)
	if !ok {
		return preview, "", false
	}
	preview.References = refs
	preview.ThreadingVersion = threadingVersion
	encoded, err := json.Marshal(preview)
	if err != nil {
		return preview, "", false
	}
	return preview, string(encoded), true
}
