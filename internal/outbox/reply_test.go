// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"context"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailsync"
	"strings"
	"testing"
)

func TestTypedOutgoingReplyKeepsOriginalReferencesAndOwner(t *testing.T) {
	service, accounts, _, _, owner, other, account := setup(t)
	ctx := context.Background()
	parent, err := service.Queue(ctx, owner, account, nonce, draft())
	if err != nil {
		t.Fatal(err)
	}
	config, err := accounts.Get(ctx, owner, account)
	if err != nil {
		t.Fatal(err)
	}
	ref := mail.SourceRef{Kind: "outbox", AccountID: account, Revision: config.Revision, OutgoingID: parent.ID}
	child, err := service.QueueReply(ctx, owner, "SYNTHETIC_REPLY_NONCE_123456789", ref, draft())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := service.RawSource(ctx, owner, account, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	original, err := service.RawSource(ctx, owner, account, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := "<" + mailsync.Parse(original.Raw).MessageID + ">"
	if !strings.Contains(string(raw.Raw), "In-Reply-To: "+id) || !strings.Contains(string(raw.Raw), "References:\r\n "+id) {
		t.Fatal("reply detached from original source")
	}
	if _, err = service.QueueReply(ctx, other, "SYNTHETIC_OTHER_NONCE_123456789", ref, draft()); err == nil {
		t.Fatal("foreign reply authorized")
	}
	ref.Revision++
	if _, err = service.QueueReply(ctx, owner, "SYNTHETIC_STALE_NONCE_123456789", ref, draft()); err == nil {
		t.Fatal("stale config replied")
	}
}
