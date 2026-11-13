// SPDX-License-Identifier: AGPL-3.0-only
package collections

import (
	"context"
	"mailspace.local/mailspace/internal/mail"
	"testing"
)

func TestBulkLabelsAndArchiveAreOwnedAtomicAndSeparateFromSpaceVetoes(t *testing.T) {
	s, db, _, owner, other, _, first, second := setup(t)
	ctx := context.Background()
	label, err := s.Create(ctx, owner, "label", "Todo", "", "blue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.LabelsMany(ctx, other, []mail.SourceRef{first}, []string{label.ID}, true); err == nil {
		t.Fatal("foreign labels accepted")
	}
	stale := second
	stale.Validity++
	if _, err = s.LabelsMany(ctx, owner, []mail.SourceRef{first, stale}, []string{label.ID}, true); err == nil {
		t.Fatal("stale batch accepted")
	}
	memberships, _ := s.Memberships(ctx, owner)
	if len(memberships) != 0 {
		t.Fatal("stale batch partially committed")
	}
	n, err := s.LabelsMany(ctx, owner, []mail.SourceRef{first, second, first}, []string{label.ID}, true)
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	n, err = s.LabelsMany(ctx, owner, []mail.SourceRef{first, second}, []string{label.ID}, true)
	if err != nil || n != 0 {
		t.Fatal("not idempotent", n, err)
	}
	var vetoes int
	if err = db.Database().QueryRow("SELECT count(*) FROM message_manual_revisions").Scan(&vetoes); err != nil || vetoes != 0 {
		t.Fatal("status labels vetoed Space automation", vetoes, err)
	}
	if _, err = s.ArchiveMany(ctx, other, []mail.SourceRef{first}); err == nil {
		t.Fatal("foreign archive accepted")
	}
	if _, err = s.ArchiveMany(ctx, owner, []mail.SourceRef{first, stale}); err == nil {
		t.Fatal("stale archive accepted")
	}
	archived, _ := s.Archived(ctx, owner)
	if len(archived) != 0 {
		t.Fatal("partial archive")
	}
	n, err = s.ArchiveMany(ctx, owner, []mail.SourceRef{first, second})
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	n, err = s.ArchiveMany(ctx, owner, []mail.SourceRef{first, second})
	if err != nil || n != 0 {
		t.Fatal("archive not idempotent", n, err)
	}
	memberships, _ = s.Memberships(ctx, owner)
	if len(memberships) != 2 {
		t.Fatal("archive erased labels")
	}
}
