// SPDX-License-Identifier: AGPL-3.0-only
package timeline

import (
	"context"
	"encoding/json"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailnet"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"mailspace.local/mailspace/internal/sharing"
	"path/filepath"
	"strings"
	"testing"
)

type checker struct{}

func (checker) CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }
func (checker) CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }
func setup(t *testing.T) (*Service, *identity.Store, *accounts.Service, *outbox.Service, string, string, accounts.Config) {
	t.Helper()
	dir := t.TempDir()
	store, err := identity.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	owner, err := store.CreateUser(context.Background(), "owner@example.test", "Synthetic timeline password")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(context.Background(), "other@example.test", "Synthetic other password")
	if err != nil {
		t.Fatal(err)
	}
	a, err := accounts.Open(store.Database(), filepath.Join(dir, "keys"), checker{})
	if err != nil {
		t.Fatal(err)
	}
	config, err := a.Save(context.Background(), owner.ID, "", accountInput())
	if err != nil {
		t.Fatal(err)
	}
	syncer := mailsync.New(store.Database(), a, nil)
	outgoing := outbox.New(store.Database(), a, syncer, nil)
	return New(store, a, syncer, outgoing), store, a, outgoing, owner.ID, other.ID, config
}
func accountInput() accounts.Input {
	return accounts.Input{Name: "Synthetic account", Address: "owner@example.test", IMAP: mailnet.Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}, SMTP: mailnet.Endpoint{Host: "smtp.example.test", Port: 465, TLS: "tls"}, IMAPLogin: mailnet.Login{Username: "owner", Password: "synthetic"}, SMTPLogin: mailnet.Login{Username: "owner", Password: "synthetic"}}
}
func put(t *testing.T, store *identity.Store, account, folder string, uid uint32, raw []byte, seen, legacy bool) {
	t.Helper()
	ctx := context.Background()
	_, err := store.Database().ExecContext(ctx, "INSERT INTO mailboxes(account_id,name,uid_validity,total,synced_at) VALUES(?,?,7,0,0) ON CONFLICT DO NOTHING", account, folder)
	if err != nil {
		t.Fatal(err)
	}
	preview := mailsync.Parse(raw)
	if legacy {
		preview.ContentHash = ""
	}
	encoded, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	if legacy {
		var value map[string]any
		json.Unmarshal(encoded, &value)
		delete(value, "ContentHash")
		encoded, _ = json.Marshal(value)
	}
	flags := `[]`
	if seen {
		flags = `["\\Seen"]`
	}
	_, err = store.Database().ExecContext(ctx, "INSERT INTO mail_messages(account_id,mailbox,uid,flags,size,received_at,raw,preview) VALUES(?,?,?,?,?,?,?,?)", account, folder, uid, flags, len(raw), int64(uid), raw, string(encoded))
	if err != nil {
		t.Fatal(err)
	}
}
func fixture(id, body, refs string) []byte {
	header := "From: Sender <sender@example.test>\r\nTo: owner@example.test\r\nSubject: Original topic\r\nContent-Type: text/plain; charset=utf-8\r\n"
	if id != "" {
		header += "Message-ID: <" + id + ">\r\n"
	}
	if refs != "" {
		header += "References: <" + refs + ">\r\n"
	}
	return []byte(header + "\r\n" + body)
}
func TestTimelineKeepsMessagesIndependentAndDeduplicatesOnlySafeCopies(t *testing.T) {
	service, store, _, _, owner, other, account := setup(t)
	ctx := context.Background()
	original := fixture("root@example.test", "Root body", "")
	put(t, store, account.ID, "INBOX", 1, original, false, true)
	put(t, store, account.ID, "Archive", 1, original, true, false)
	put(t, store, account.ID, "Sent", 2, fixture("reply@example.test", "Independent reply", "root@example.test"), true, false)
	put(t, store, account.ID, "INBOX", 3, fixture("root@example.test", "Conflicting duplicate ID", ""), false, false)
	put(t, store, account.ID, "INBOX", 4, fixture("", "No identifier", ""), true, false)
	put(t, store, account.ID, "INBOX", 5, fixture("", "No identifier", ""), true, false)
	records, err := service.List(ctx, owner, "")
	if err != nil || len(records) != 5 {
		t.Fatal("message grouping/copy identity incorrect", len(records), err)
	}
	var root Record
	for _, record := range records {
		if record.Preview.Text == "Root body" {
			root = record
		}
	}
	if root.Ref.Mailbox != "INBOX" || !root.Unread || root.Preview.ContentHash == "" {
		t.Fatal("canonical unread copy or legacy hash lost", root.Ref)
	}
	identity, err := service.Identity(ctx, owner, root.Ref)
	if err != nil || identity != root.Identity {
		t.Fatal("list/resolver identity differs", err)
	}
	if _, err = service.Resolve(ctx, other, root.Ref); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign message resolved")
	}
	if _, err = service.List(ctx, other, account.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign account filter resolved")
	}
	stale := root.Ref
	stale.Revision++
	if _, err = service.Resolve(ctx, owner, stale); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale config reference resolved")
	}
	stale = root.Ref
	stale.Validity++
	if _, err = service.Resolve(ctx, owner, stale); !errors.Is(err, ErrNotFound) {
		t.Fatal("stale UIDVALIDITY reference resolved")
	}
}
func TestOutgoingAppearsImmediatelyAndCollapsesWithExactSentCopy(t *testing.T) {
	service, store, a, outgoing, owner, _, account := setup(t)
	ctx := context.Background()
	record, err := outgoing.Queue(ctx, owner, account.ID, "SYNTHETIC_TIMELINE_REQUEST_12345", outbox.Draft{To: "recipient@example.test", Subject: "Outgoing", Body: "Private ordinary mail", AllowPlaintext: true})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := service.List(ctx, owner, "")
	if err != nil || len(entries) != 1 || entries[0].Ref.Kind != "outbox" || entries[0].State != "pending" || !entries[0].Own {
		t.Fatal("pending outbox was hidden or mislabeled", err)
	}
	source, err := outgoing.RawSource(ctx, owner, account.ID, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	put(t, store, account.ID, "Sent", 1, source.Raw, true, false)
	if _, err = store.Database().Exec("UPDATE outbox SET state='accepted',copy_state='saved' WHERE id=?", record.ID); err != nil {
		t.Fatal(err)
	}
	entries, err = service.List(ctx, owner, "")
	if err != nil || len(entries) != 1 || entries[0].Ref.Kind != "imap" || entries[0].State != "accepted" {
		t.Fatal("Sent/outbox copy duplicated or lost state", err)
	}
	before := entries[0].Ref
	if _, err = a.Save(ctx, owner, account.ID, accountInput()); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Resolve(ctx, owner, before); !errors.Is(err, ErrNotFound) {
		t.Fatal("connection change reused old source reference")
	}
	entries, err = service.List(ctx, owner, "")
	if err != nil || len(entries) != 0 {
		t.Fatal("old outbox config mixed into new account feed", err)
	}
}
func TestLogicalIdentitySeparatesAccountsAndConfiguration(t *testing.T) {
	p := mailsync.Parse(fixture("same@example.test", "Same bytes", ""))
	ref := mail.SourceRef{Kind: "imap", Mailbox: "INBOX", Validity: 1, UID: 1}
	a := identityFor("account-one", 1, p, ref)
	b := identityFor("account-two", 1, p, ref)
	c := identityFor("account-one", 2, p, ref)
	if a.Key == b.Key || a.Key == c.Key || len(a.Key) != 64 {
		t.Fatal("logical identity crossed namespace")
	}
	p.MessageID = ""
	other := ref
	other.UID = 2
	if identityFor("one", 1, p, ref) == identityFor("one", 1, p, other) {
		t.Fatal("missing IDs collapsed physical messages")
	}
	if strings.Contains(fmtRecord(Record{Preview: p}), "Same bytes") {
		t.Fatal("source logging exposes mail")
	}
}
func fmtRecord(record Record) string { return record.String() }

func TestSharedGrantIsReadOnlyFolderScopedAndRevocable(t *testing.T) {
	service, store, a, _, owner, other, account := setup(t)
	ctx := context.Background()
	put(t, store, account.ID, "INBOX", 1, fixture("shared@example.test", "Shared body", ""), false, false)
	put(t, store, account.ID, "Archive", 1, fixture("archived@example.test", "Archived body", ""), true, false)
	workspace, err := store.CreateWorkspace(ctx, owner, "Team")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.AddMember(ctx, owner, workspace.ID, "other@example.test"); err != nil {
		t.Fatal(err)
	}
	grants := sharing.New(store, a)
	service.SetSharing(grants)
	if _, err = service.ListForViewer(ctx, other, account.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("ungranted account listed")
	}
	if _, err = grants.Grant(ctx, owner, workspace.ID, account.ID, "INBOX", "other@example.test", false); err != nil {
		t.Fatal(err)
	}
	records, err := service.ListForViewer(ctx, other, "")
	if err != nil || len(records) != 1 || !records[0].Shared || records[0].SharedBy != "owner@example.test" || records[0].CanDownload {
		t.Fatalf("shared list incorrect: %#v (%v)", records, err)
	}
	if records[0].Ref.Mailbox != "INBOX" {
		t.Fatal("folder-scoped grant widened to another folder")
	}
	resolved, err := service.ResolveForViewer(ctx, other, records[0].Ref)
	if err != nil || !resolved.Record.Shared || resolved.Record.CanDownload {
		t.Fatal("shared resolve incorrect", err)
	}
	// The owner-scoped path used by mutations never resolves another owner's source.
	if _, err = service.Resolve(ctx, other, records[0].Ref); !errors.Is(err, ErrNotFound) {
		t.Fatal("owner-scoped resolve leaked a shared source")
	}
	// The owner's own list is unchanged and never marked shared.
	owned, err := service.List(ctx, owner, "")
	if err != nil || len(owned) != 2 {
		t.Fatal("owner list changed", len(owned), err)
	}
	for _, record := range owned {
		if record.Shared {
			t.Fatal("owned record marked shared")
		}
	}
	// A local outbox source is never shared, even with a whole-account grant.
	outboxRef := mail.SourceRef{Kind: "outbox", AccountID: account.ID, Revision: records[0].Ref.Revision, OutgoingID: "0123456789abcdef0123456789abcdef"}
	if _, err = service.ResolveForViewer(ctx, other, outboxRef); !errors.Is(err, ErrNotFound) {
		t.Fatal("shared outbox exposed")
	}
	if _, err = grants.Grant(ctx, owner, workspace.ID, account.ID, "", "other@example.test", true); err != nil {
		t.Fatal(err)
	}
	if resolved, err = service.ResolveForViewer(ctx, other, records[0].Ref); err != nil || !resolved.Record.CanDownload {
		t.Fatal("download grant not reflected", err)
	}
	if list, err := service.ListForViewer(ctx, other, ""); err != nil || len(list) != 2 {
		t.Fatal("whole-account grant did not cover every folder", len(list), err)
	}
	if err = grants.Revoke(ctx, owner, records[0].Ref.AccountID); !errors.Is(err, sharing.ErrNotFound) {
		t.Fatal("revoke by account id accepted")
	}
	grantsList, err := grants.List(ctx, owner)
	if err != nil || len(grantsList) != 2 {
		t.Fatal("owner grant list incorrect", len(grantsList), err)
	}
	for _, item := range grantsList {
		if err = grants.Revoke(ctx, owner, item.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = service.ResolveForViewer(ctx, other, records[0].Ref); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked grant still resolved")
	}
}
