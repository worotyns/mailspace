// SPDX-License-Identifier: AGPL-3.0-only
package rules

import (
	"context"
	"encoding/json"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailnet"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"mailspace.local/mailspace/internal/timeline"
	"path/filepath"
	"testing"
)

type checker struct{}

func (checker) CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }
func (checker) CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }

type fixture struct {
	engine                      *Service
	db                          *identity.Store
	groups                      *collections.Service
	source                      *timeline.Service
	owner, other, space, second string
	ref                         mail.SourceRef
}

func setup(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := identity.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a, err := db.CreateUser(ctx, "alice@example.test", "Synthetic Alice rule password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateUser(ctx, "bob@example.test", "Synthetic Bob rule password")
	if err != nil {
		t.Fatal(err)
	}
	accountsService, err := accounts.Open(db.Database(), filepath.Join(dir, "keys"), checker{})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := accountsService.Save(ctx, a.ID, "", accounts.Input{Name: "Rule fixture", Address: a.Email, IMAP: mailnet.Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}, SMTP: mailnet.Endpoint{Host: "smtp.example.test", Port: 465, TLS: "tls"}, IMAPLogin: mailnet.Login{Username: "alice", Password: "synthetic"}, SMTPLogin: mailnet.Login{Username: "alice", Password: "synthetic"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Database().Exec("INSERT INTO mailboxes(account_id,name,uid_validity,total,synced_at) VALUES(?,'INBOX',7,1,0)", cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: billing@payments.test\r\nTo: alice@example.test\r\nSubject: Invoice TEST-42\r\nMessage-ID: <rules@example.test>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nPRIVATE_RULE_BODY_CANARY")
	preview, _ := json.Marshal(mailsync.Parse(raw))
	_, err = db.Database().Exec("INSERT INTO mail_messages(account_id,mailbox,uid,flags,size,received_at,raw,preview) VALUES(?,'INBOX',1,'[]',?,1,?,?)", cfg.ID, len(raw), raw, string(preview))
	if err != nil {
		t.Fatal(err)
	}
	syncer := mailsync.New(db.Database(), accountsService, nil)
	box := outbox.New(db.Database(), accountsService, syncer, nil)
	sources := timeline.New(db, accountsService, syncer, box)
	groups := collections.New(db.Database(), sources)
	space, err := groups.Create(ctx, a.ID, "space", "Payments", "Payment topics", "blue")
	if err != nil {
		t.Fatal(err)
	}
	second, err := groups.Create(ctx, a.ID, "space", "Work", "Work topics", "green")
	if err != nil {
		t.Fatal(err)
	}
	return fixture{New(db.Database(), sources), db, groups, sources, a.ID, b.ID, space.ID, second.ID, mail.SourceRef{Kind: "imap", AccountID: cfg.ID, Revision: cfg.Revision, Mailbox: "INBOX", Validity: 7, UID: 1}}
}
func TestRulesDisabledPreviewApplyIdempotenceAndManualOverride(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	r, err := f.engine.Save(ctx, f.owner, "", "sender", "billing@payments.test", f.space, 0)
	if err != nil || r.Enabled {
		t.Fatal("rule not saved disabled", err)
	}
	if n, err := f.engine.Process(ctx, f.owner, ""); err != nil || n != 0 {
		t.Fatal("disabled rule applied")
	}
	preview, err := f.engine.Preview(ctx, f.owner, r.ID)
	if err != nil || len(preview.Messages) != 1 {
		t.Fatal("disabled dry-run unavailable", err)
	}
	var count int
	f.db.Database().QueryRow("SELECT count(*) FROM message_collection_memberships").Scan(&count)
	if count != 0 {
		t.Fatal("preview changed membership")
	}
	if err = f.engine.Enable(ctx, f.owner, r.ID, r.Revision, true); err != nil {
		t.Fatal(err)
	}
	if n, err := f.engine.Process(ctx, f.owner, ""); err != nil || n != 1 {
		t.Fatal("rule did not apply", n, err)
	}
	var source string
	if err = f.db.Database().QueryRow("SELECT source FROM message_collection_memberships").Scan(&source); err != nil || source != "rule" {
		t.Fatal("rule mislabeled as manual/model")
	}
	if n, err := f.engine.Process(ctx, f.owner, ""); err != nil || n != 0 {
		t.Fatal("rule replay repeated effects", n, err)
	}
	if err = f.groups.Assign(ctx, f.owner, "space", f.space, f.ref, false); err != nil {
		t.Fatal(err)
	}
	r, err = f.engine.Get(ctx, f.owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.engine.Save(ctx, f.owner, r.ID, r.Kind, r.Value, r.SpaceID, r.Revision); err != nil {
		t.Fatal(err)
	}
	r, _ = f.engine.Get(ctx, f.owner, r.ID)
	if r.Enabled {
		t.Fatal("edited rule stayed active without review")
	}
	if err = f.engine.Enable(ctx, f.owner, r.ID, r.Revision, true); err != nil {
		t.Fatal(err)
	}
	if n, err := f.engine.Process(ctx, f.owner, ""); err != nil || n != 0 {
		t.Fatal("manual veto lost to edited rule")
	}
	preview, err = f.engine.Preview(ctx, f.owner, r.ID)
	if err != nil || len(preview.Messages) != 0 || preview.ManualSkipped != 1 {
		t.Fatal("manual exception missing from preview")
	}
}
func TestRuleScopeRevocationAndMultipleMatchingSpaces(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first, err := f.engine.Save(ctx, f.owner, "", "domain", "payments.test", f.space, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.engine.Save(ctx, f.other, "", "sender", "billing@payments.test", f.space, 0); !errors.Is(err, ErrChanged) {
		t.Fatal("foreign Space accepted")
	}
	if _, err = f.engine.Get(ctx, f.other, first.ID); err == nil {
		t.Fatal("foreign rule read")
	}
	if err = f.engine.Enable(ctx, f.other, first.ID, first.Revision, true); err == nil {
		t.Fatal("foreign rule enabled")
	}
	if err = f.engine.Delete(ctx, f.other, first.ID, first.Revision); err == nil {
		t.Fatal("foreign rule deleted")
	}
	second, err := f.engine.Save(ctx, f.owner, "", "subject", "invoice", f.second, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []Rule{first, second} {
		if err = f.engine.Enable(ctx, f.owner, r.ID, r.Revision, true); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := f.engine.Process(ctx, f.owner, ""); err != nil || n != 2 {
		t.Fatal("many-to-many matching failed", n, err)
	}
	first, _ = f.engine.Get(ctx, f.owner, first.ID)
	if err = f.engine.Enable(ctx, f.owner, first.ID, first.Revision, false); err != nil {
		t.Fatal(err)
	}
	record, err := f.source.Resolve(ctx, f.owner, f.ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.engine.apply(ctx, f.owner, first, record.Record); !errors.Is(err, ErrChanged) {
		t.Fatal("disabled in-flight rule applied")
	}
	second, _ = f.engine.Get(ctx, f.owner, second.ID)
	if err = f.engine.Delete(ctx, f.owner, second.ID, second.Revision); err != nil {
		t.Fatal(err)
	}
	var count int
	f.db.Database().QueryRow("SELECT count(*) FROM message_collection_memberships").Scan(&count)
	if count != 2 {
		t.Fatal("removing rule deleted previous assignments")
	}
}
func TestRuleMatchingNeverReadsBodyAndRejectsEncryptedOwnOrUnsupported(t *testing.T) {
	m := timeline.Record{Preview: mailsync.Preview{Sender: "sender@sub.example.test", Subject: "Ordinary subject", Text: "PRIVATE_BODY_INVOICE", Status: "ready"}}
	if Matches(Rule{Kind: "subject", Value: "PRIVATE_BODY_INVOICE"}, m) {
		t.Fatal("rule inspected body")
	}
	if Matches(Rule{Kind: "domain", Value: "example.test"}, m) {
		t.Fatal("exact domain expanded to subdomains")
	}
	if !Matches(Rule{Kind: "domain", Value: "sub.example.test"}, m) {
		t.Fatal("exact domain not matched")
	}
	for _, change := range []func(*timeline.Record){func(m *timeline.Record) { m.Own = true }, func(m *timeline.Record) { m.Preview.Encrypted = true }, func(m *timeline.Record) { m.Preview.Status = "pending" }} {
		copy := m
		change(&copy)
		if Matches(Rule{Kind: "sender", Value: copy.Preview.Sender}, copy) {
			t.Fatal("ineligible mail matched")
		}
	}
}

func TestRuleDomainValidation(t *testing.T) {
	for _, value := range []string{".example.test", "example..test", "-example.test", "example-.test", "example.test/path", "example\t.test", "*.example.test", "éxample.test"} {
		if fields("domain", value) {
			t.Errorf("invalid domain accepted: %q", value)
		}
	}
	for _, value := range []string{"example.test", "SUB.Example.test", "xn--bcher-kva.test"} {
		if !fields("domain", value) {
			t.Errorf("valid exact domain rejected: %q", value)
		}
	}
}

func TestExpressionRulesKeepOwnershipConsentAndEncryptedExclusion(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	query := `(subject:invoice OR subject:receipt) AND body:PRIVATE_RULE_BODY_CANARY`
	if _, err := f.engine.SaveExpression(ctx, f.other, "", query, f.space, 0); err == nil {
		t.Fatal("foreign expression target accepted")
	}
	rule, err := f.engine.SaveExpression(ctx, f.owner, "", query, f.space, 0)
	if err != nil || rule.Enabled {
		t.Fatal("expression not saved disabled", err)
	}
	if _, err := f.engine.Get(ctx, f.other, rule.ID); err == nil {
		t.Fatal("foreign expression readable")
	}
	preview, err := f.engine.Preview(ctx, f.owner, rule.ID)
	if err != nil || len(preview.Messages) != 1 {
		t.Fatal("expression preview failed", err)
	}
	encrypted := preview.Messages[0]
	encrypted.Preview.Encrypted = true
	if Matches(rule, encrypted) {
		t.Fatal("encrypted rule body matched")
	}
	if n, err := f.engine.Process(ctx, f.owner, ""); err != nil || n != 0 {
		t.Fatal("disabled expression executed")
	}
	if err = f.engine.Enable(ctx, f.owner, rule.ID, rule.Revision, true); err != nil {
		t.Fatal(err)
	}
	if n, err := f.engine.Process(ctx, f.owner, ""); err != nil || n != 1 {
		t.Fatal("approved expression failed", n, err)
	}
	rule, _ = f.engine.Get(ctx, f.owner, rule.ID)
	changed, err := f.engine.SaveExpression(ctx, f.owner, rule.ID, `subject:other`, f.space, rule.Revision)
	if err != nil || changed.Enabled || changed.Revision == rule.Revision {
		t.Fatal("edit kept old automation active", err)
	}
	if _, err = f.engine.SaveExpression(ctx, f.owner, rule.ID, `body:x OR`, f.space, changed.Revision); err == nil {
		t.Fatal("invalid expression stored")
	}
}
