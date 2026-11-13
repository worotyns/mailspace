// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"context"
	"mailspace.local/mailspace/internal/mail"
)

// QueueReply binds the reply to one original source, never a Space's previous
// timeline entry. Configuration changes cannot reuse an old composer reference.
func (s *Service) QueueReply(ctx context.Context, owner, nonce string, ref mail.SourceRef, draft Draft) (Record, error) {
	if ref.AccountID == "" || ref.Revision < 1 {
		return Record{}, ErrInput
	}
	draft.SourceRevision = ref.Revision
	switch ref.Kind {
	case "imap":
		if ref.UID == 0 || ref.Validity == 0 || ref.Mailbox == "" || ref.OutgoingID != "" {
			return Record{}, ErrInput
		}
		draft.ReplyFolder = ref.Mailbox
		draft.ReplyUID = ref.UID
		draft.ReplyValidity = ref.Validity
	case "outbox":
		if ref.OutgoingID == "" || ref.UID != 0 || ref.Validity != 0 || ref.Mailbox != "" {
			return Record{}, ErrInput
		}
		draft.ReplyOutgoing = ref.OutgoingID
	default:
		return Record{}, ErrInput
	}
	return s.Queue(ctx, owner, ref.AccountID, nonce, draft)
}
