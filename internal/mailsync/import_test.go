// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"context"
	"errors"
	"mailspace.local/mailspace/internal/mailnet"
	"testing"
	"time"
)

func TestFreshStartSkipsHistorySurvivesRestartAndExplicitImport(t *testing.T) {
	s, a, db, owner, other, id, f := setup(t)
	ctx := context.Background()
	if err := s.SetImportMode(ctx, other, id, "recent"); err == nil {
		t.Fatal("foreign user changed import policy")
	}
	if err := s.SetImportMode(ctx, owner, id, "fresh"); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	messages, err := s.Messages(ctx, owner, id, "INBOX")
	if err != nil || len(messages) != 0 || f.downloads != 0 {
		t.Fatal("fresh setup downloaded existing bodies")
	}
	f.boxes[0].Messages = append(f.boxes[0].Messages, mailnet.RemoteMessage{UID: 3, Size: 200, Received: time.Unix(300, 0)})
	f.boxes[0].Total = 3
	f.raw[3] = synthetic(3, "")
	if err = s.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	messages, _ = s.Messages(ctx, owner, id, "INBOX")
	if len(messages) != 1 || messages[0].UID != 3 || f.downloads != 1 {
		t.Fatal("new incoming mail not synced")
	}
	restarted := New(db.Database(), a, f)
	if err = restarted.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	if f.downloads != 1 {
		t.Fatal("restart imported historical mail or re-downloaded current mail")
	}
	if err = restarted.SetImportMode(ctx, owner, id, "recent"); err != nil {
		t.Fatal(err)
	}
	if err = restarted.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	messages, _ = s.Messages(ctx, owner, id, "INBOX")
	if len(messages) != 3 || f.downloads != 3 {
		t.Fatal("explicit recent import did not fetch skipped bodies")
	}
	if err = restarted.SetImportMode(ctx, owner, id, "fresh"); err != nil {
		t.Fatal(err)
	}
	if err = restarted.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	messages, _ = s.Messages(ctx, owner, id, "INBOX")
	if len(messages) != 3 || f.downloads != 3 {
		t.Fatal("switching to fresh mode discarded already cached mail")
	}
	f.boxes[0].UIDValidity = 2
	if err = restarted.Sync(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	messages, _ = s.Messages(ctx, owner, id, "INBOX")
	if len(messages) != 0 || f.downloads != 3 {
		t.Fatal("UIDVALIDITY reset reused old cache/baseline or silently imported history")
	}
}
func TestImportPolicyChangeRejectsInFlightCommit(t *testing.T) {
	s, a, _, owner, _, id, f := setup(t)
	ctx := context.Background()
	if err := s.SetImportMode(ctx, owner, id, "fresh"); err != nil {
		t.Fatal(err)
	}
	old, err := s.ImportPolicy(ctx, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := a.Get(ctx, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetImportMode(ctx, owner, id, "recent"); err != nil {
		t.Fatal(err)
	}
	err = s.apply(ctx, owner, id, cfg.Revision, f.boxes[0], nil, nil, old.Revision)
	if !errors.Is(err, ErrChanged) {
		t.Fatal("stale import committed after policy change", err)
	}
}
