// SPDX-License-Identifier: AGPL-3.0-only
package timeline

import (
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailsync"
	"testing"
)

func TestRelatedUsesUniqueReferencesWithinCurrentAccount(t *testing.T) {
	record := func(key, id, account string, refs ...string) Record {
		return Record{Identity: mail.MessageIdentity{Key: key}, Ref: mail.SourceRef{AccountID: account, Revision: 1}, Preview: mailsync.Preview{MessageID: id, References: refs, Subject: "same subject"}}
	}
	records := []Record{record("root", "a", "one"), record("reply", "b", "one", "a"), record("other", "c", "one"), record("foreign", "d", "two", "a")}
	related, err := Related(records, "reply")
	if err != nil || len(related) != 2 {
		t.Fatal("references/account scope failed", len(related), err)
	}
	records = append(records, record("ambiguous", "a", "one"))
	if related, err = Related(records, "reply"); err != nil || len(related) != 1 {
		t.Fatal("ambiguous ID guessed a thread")
	}
	if _, err = Related(records, "not-owned"); err != ErrNotFound {
		t.Fatal("missing source accepted")
	}
	old := record("old", "e", "one", "b")
	old.Ref.Revision = 2
	records = append(records, old)
	if related, err = Related(records, "old"); err != nil || len(related) != 1 {
		t.Fatal("stale connection joined current thread")
	}
	cycle := []Record{record("x", "x", "one", "y"), record("y", "y", "one", "x")}
	if related, err = Related(cycle, "x"); err != nil || len(related) != 2 {
		t.Fatal("cycle did not terminate")
	}
}

func TestRelatedSharesMissingReferenceButNeverInventsParentOrMergesLocalizedSubjects(t *testing.T) {
	record := func(key, id, account, subject string, refs ...string) Record {
		return Record{Identity: mail.MessageIdentity{Key: key}, Ref: mail.SourceRef{AccountID: account, Revision: 1}, Preview: mailsync.Preview{MessageID: id, References: refs, Subject: subject}}
	}
	records := []Record{record("a", "reply-a", "owned", "Siema", "missing-root"), record("b", "reply-b", "owned", "Odp: Siema", "missing-root"), record("same-subject", "other", "owned", "Siema"), record("foreign", "foreign", "other-account", "Odp: Siema", "missing-root")}
	old := record("old", "old", "owned", "Odp: Siema", "missing-root")
	old.Ref.Revision = 2
	records = append(records, old)
	related, err := Related(records, "b")
	if err != nil || len(related) != 2 || related[0].Identity.Key != "a" || related[1].Identity.Key != "b" {
		t.Fatal("explicit missing-root descendants not related", related, err)
	}
	if _, err = Related(records, "missing-root"); err != ErrNotFound {
		t.Fatal("uncached ancestor invented a reply target")
	}
	records = append(records, record("duplicate-one", "missing-root", "owned", "Siema"), record("duplicate-two", "missing-root", "owned", "Siema"))
	related, err = Related(records, "b")
	if err != nil || len(related) != 1 {
		t.Fatal("ambiguous ancestor joined replies", related, err)
	}
}
