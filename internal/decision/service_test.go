// SPDX-License-Identifier: AGPL-3.0-only
package decision

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
	"mailspace.local/mailspace/internal/privacy"
	"mailspace.local/mailspace/internal/timeline"
	"path/filepath"
	"strings"
	"testing"
)

type checker struct{}

func (checker) CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }
func (checker) CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error { return nil }

type stub struct {
	calls int
	input Input
	fn    func(Input) (Proposal, error)
}

func (p *stub) Name() string { return "Synthetic local classifier" }
func (p *stub) Propose(_ context.Context, input Input) (Proposal, error) {
	p.calls++
	p.input = input
	return p.fn(input)
}

type fixture struct {
	engine        *Service
	db            *identity.Store
	groups        *collections.Service
	accounts      *accounts.Service
	owner, other  string
	ref           mail.SourceRef
	provider      *stub
	space, second collections.Item
	config        accounts.Config
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
	owner, err := db.CreateUser(ctx, "owner@example.test", "Synthetic decision password")
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateUser(ctx, "other@example.test", "Synthetic other password")
	if err != nil {
		t.Fatal(err)
	}
	a, err := accounts.Open(db.Database(), filepath.Join(dir, "keys"), checker{})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := a.Save(ctx, owner.ID, "", accounts.Input{Name: "Synthetic mail", Address: owner.Email, IMAP: mailnet.Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}, SMTP: mailnet.Endpoint{Host: "smtp.example.test", Port: 465, TLS: "tls"}, IMAPLogin: mailnet.Login{Username: "owner", Password: "synthetic"}, SMTPLogin: mailnet.Login{Username: "owner", Password: "synthetic"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Database().Exec("INSERT INTO mailboxes(account_id,name,uid_validity,total,synced_at) VALUES(?,'INBOX',7,1,0)", cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: sender@example.test\r\nTo: owner@example.test\r\nSubject: PRIVATE_SUBJECT_CANARY\r\nMessage-ID: <one@example.test>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nPRIVATE_BODY_CANARY")
	preview, _ := json.Marshal(mailsync.Parse(raw))
	_, err = db.Database().Exec("INSERT INTO mail_messages(account_id,mailbox,uid,flags,size,received_at,raw,preview) VALUES(?,'INBOX',1,'[]',?,1,?,?)", cfg.ID, len(raw), raw, string(preview))
	if err != nil {
		t.Fatal(err)
	}
	syncer := mailsync.New(db.Database(), a, nil)
	box := outbox.New(db.Database(), a, syncer, nil)
	sources := timeline.New(db, a, syncer, box)
	groups := collections.New(db.Database(), sources)
	space, err := groups.Create(ctx, owner.ID, "space", "Payments", "Payments and billing", "blue")
	if err != nil {
		t.Fatal(err)
	}
	second, err := groups.Create(ctx, owner.ID, "space", "Commerce", "Commerce topics", "green")
	if err != nil {
		t.Fatal(err)
	}
	provider := &stub{fn: func(Input) (Proposal, error) {
		return Proposal{SpaceIDs: []string{space.ID, second.ID}, Confidence: .95}, nil
	}}
	return fixture{New(db.Database(), sources, groups, provider), db, groups, a, owner.ID, other.ID, mail.SourceRef{Kind: "imap", AccountID: cfg.ID, Revision: cfg.Revision, Mailbox: "INBOX", Validity: 7, UID: 1}, provider, space, second, cfg}
}
func TestReviewDryRunApplyReplayAndPrivateTrace(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.engine.Evaluate(ctx, f.owner, f.ref, true); !errors.Is(err, ErrDisabled) || f.provider.calls != 0 {
		t.Fatal("disabled classifier called provider")
	}
	if err := f.engine.Save(ctx, f.owner, "review", "metadata"); err != nil {
		t.Fatal(err)
	}
	run, err := f.engine.Evaluate(ctx, f.owner, f.ref, true)
	if err != nil || run.State != "dry-run" {
		t.Fatal(run.State, err)
	}
	if f.provider.input.Text != "" || f.provider.input.Subject != "PRIVATE_SUBJECT_CANARY" {
		t.Fatal("metadata payload policy failed")
	}
	memberships, _ := f.groups.Memberships(ctx, f.owner)
	if len(memberships) != 0 {
		t.Fatal("dry run assigned messages")
	}
	if err = f.engine.Replay(ctx, f.owner, run.ID); err != nil || f.provider.calls != 1 {
		t.Fatal("replay called provider", err)
	}
	if err = f.engine.Apply(ctx, f.other, run.ID, true); err == nil {
		t.Fatal("foreign approval")
	}
	if err = f.engine.Apply(ctx, f.owner, run.ID, true); err != nil {
		t.Fatal(err)
	}
	if err = f.engine.Apply(ctx, f.owner, run.ID, true); err != nil {
		t.Fatal("non-idempotent replay", err)
	}
	memberships, _ = f.groups.Memberships(ctx, f.owner)
	if len(memberships) != 1 {
		t.Fatal("source identity lost")
	}
	for _, ids := range memberships {
		if len(ids) != 2 {
			t.Fatal("many-to-many proposal not applied")
		}
	}
	var trace string
	if err = f.db.Database().QueryRow("SELECT snapshot||decision FROM decision_runs WHERE id=?", run.ID).Scan(&trace); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"PRIVATE_BODY_CANARY", "PRIVATE_SUBJECT_CANARY", "sender@example.test", "Payments and billing"} {
		if strings.Contains(trace, private) {
			t.Fatal("private data entered trace")
		}
	}
	if _, err = f.engine.Evaluate(ctx, f.owner, f.ref, false); !errors.Is(err, ErrChanged) {
		t.Fatal("human approval did not protect manual authority")
	}
}
func TestAutomaticAssignmentsRespectLateHumanAndDefinitionChanges(t *testing.T) {
	for _, change := range []string{"manual", "definition", "settings", "invalid-id", "duplicate-id", "low-confidence", "provider-error"} {
		t.Run(change, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			if err := f.engine.Save(ctx, f.owner, "auto", "text"); err != nil {
				t.Fatal(err)
			}
			f.provider.fn = func(Input) (Proposal, error) {
				p := Proposal{SpaceIDs: []string{f.space.ID}, Confidence: .9}
				switch change {
				case "manual":
					if err := f.groups.Assign(ctx, f.owner, "space", f.second.ID, f.ref, true); err != nil {
						t.Fatal(err)
					}
				case "definition":
					if _, err := f.groups.Update(ctx, f.owner, "space", f.space.ID, "Payments", "Changed brief", "blue", f.space.Revision); err != nil {
						t.Fatal(err)
					}
				case "settings":
					if err := f.engine.Save(ctx, f.owner, "off", "metadata"); err != nil {
						t.Fatal(err)
					}
				case "invalid-id":
					p.SpaceIDs = []string{"foreign-or-unknown"}
				case "duplicate-id":
					p.SpaceIDs = []string{f.space.ID, f.space.ID}
				case "low-confidence":
					p.Confidence = .4
				case "provider-error":
					return p, errors.New("PRIVATE_PROVIDER_ERROR_CANARY")
				}
				return p, nil
			}
			_, _ = f.engine.Evaluate(ctx, f.owner, f.ref, false)
			if f.provider.input.Text != "PRIVATE_BODY_CANARY" {
				t.Fatal("ordinary text opt-in ignored")
			}
			memberships, _ := f.groups.Memberships(ctx, f.owner)
			if change == "manual" {
				for _, ids := range memberships {
					if len(ids) != 1 || ids[0] != f.second.ID {
						t.Fatal("late model overwrote human")
					}
				}
			} else if len(memberships) != 0 {
				t.Fatal("stale/invalid decision applied")
			}
		})
	}
}
func TestAutoCycleIsIdempotentAndManualPromotionWins(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.engine.Save(ctx, f.owner, "auto", "metadata"); err != nil {
		t.Fatal(err)
	}
	f.engine.Cycle(ctx)
	f.engine.Cycle(ctx)
	if f.provider.calls != 1 {
		t.Fatal("worker reclassified unchanged source", f.provider.calls)
	}
	runs, err := f.engine.Runs(ctx, f.owner)
	if err != nil || len(runs) != 1 || runs[0].State != "applied" {
		t.Fatal("automatic job did not apply", err)
	}
	if err = f.groups.Assign(ctx, f.owner, "space", f.space.ID, f.ref, true); err != nil {
		t.Fatal(err)
	}
	if _, err = f.engine.Evaluate(ctx, f.owner, f.ref, false); !errors.Is(err, ErrChanged) {
		t.Fatal("explicit manual confirmation of model choice was ignored")
	}
	if _, err = f.engine.Evaluate(ctx, f.other, f.ref, true); err == nil {
		t.Fatal("foreign source exposed")
	}
}
func TestEncryptedAndOwnMailNeverReachProvider(t *testing.T) {
	for _, kind := range []string{"encrypted", "own"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			_ = f.engine.Save(ctx, f.owner, "auto", "text")
			sender := "sender@example.test"
			body := "-----BEGIN PGP MESSAGE-----\nCIPHERTEXT_CANARY"
			if kind == "own" {
				sender = "owner@example.test"
				body = "own ordinary content"
			}
			raw := []byte("From: " + sender + "\r\nTo: owner@example.test\r\nMessage-ID: <replacement@example.test>\r\nContent-Type: text/plain\r\n\r\n" + body)
			preview, _ := json.Marshal(mailsync.Parse(raw))
			_, err := f.db.Database().Exec("UPDATE mail_messages SET raw=?,preview=? WHERE account_id=?", raw, string(preview), f.config.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.engine.Evaluate(ctx, f.owner, f.ref, true); err == nil {
				t.Fatal("ineligible source accepted")
			}
			f.engine.Cycle(ctx)
			if f.provider.calls != 0 {
				t.Fatal("ineligible mail uploaded")
			}
		})
	}
}

func TestExplicitClientSelectionRequiresScopedConsentAndNeverAutoApplies(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_ = f.engine.Save(ctx, f.owner, "auto", "metadata")
	raw := []byte("From: sender@example.test\r\nTo: owner@example.test\r\nMessage-ID: <encrypted@example.test>\r\nContent-Type: text/plain\r\n\r\n-----BEGIN PGP MESSAGE-----\nCIPHERTEXT_ONLY")
	preview, _ := json.Marshal(mailsync.Parse(raw))
	_, err := f.db.Database().Exec("UPDATE mail_messages SET raw=?,preview=? WHERE account_id=?", raw, string(preview), f.config.ID)
	if err != nil {
		t.Fatal(err)
	}
	consent := privacy.ClientClassificationConsent{Provider: f.engine.ProviderName(), Purpose: "classify-selected-excerpt", Approved: true}
	denied := consent
	denied.Approved = false
	if _, err = f.engine.EvaluateSelection(ctx, f.owner, f.ref, "APPROVED_EXCERPT", "SYNTHETIC_SELECTION_REQUEST_12345", denied); err == nil || f.provider.calls != 0 {
		t.Fatal("no-consent export accepted")
	}
	denied = consent
	denied.Provider = "other destination"
	if _, err = f.engine.EvaluateSelection(ctx, f.owner, f.ref, "APPROVED_EXCERPT", "SYNTHETIC_SELECTION_REQUEST_12345", denied); err == nil || f.provider.calls != 0 {
		t.Fatal("wrong destination accepted")
	}
	if _, err = f.engine.EvaluateSelection(ctx, f.other, f.ref, "APPROVED_EXCERPT", "SYNTHETIC_SELECTION_REQUEST_12345", consent); err == nil {
		t.Fatal("foreign selection accepted")
	}
	run, err := f.engine.EvaluateSelection(ctx, f.owner, f.ref, "APPROVED_EXCERPT", "SYNTHETIC_SELECTION_REQUEST_12345", consent)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "dry-run" || f.provider.calls != 1 || f.provider.input.Text != "APPROVED_EXCERPT" || f.provider.input.Subject != "" || f.provider.input.From != "" || len(f.provider.input.To) != 0 {
		t.Fatal("selection-only boundary failed")
	}
	var trace string
	_ = f.db.Database().QueryRow("SELECT snapshot||decision FROM decision_runs WHERE id=?", run.ID).Scan(&trace)
	if strings.Contains(trace, "APPROVED_EXCERPT") || strings.Contains(trace, "CIPHERTEXT_ONLY") {
		t.Fatal("selection entered trace")
	}
	memberships, _ := f.groups.Memberships(ctx, f.owner)
	if len(memberships) != 0 {
		t.Fatal("encrypted selection auto-assigned")
	}
	if err = f.engine.Apply(ctx, f.owner, run.ID, false); err == nil {
		t.Fatal("automatic encrypted apply allowed")
	}
	if err = f.engine.Replay(ctx, f.owner, run.ID); err != nil || f.provider.calls != 1 {
		t.Fatal("replay exported selection again")
	}
	if err = f.engine.Apply(ctx, f.owner, run.ID, true); err != nil {
		t.Fatal("explicit human approval failed", err)
	}
	f.engine.Cycle(ctx)
	if f.provider.calls != 1 {
		t.Fatal("worker picked encrypted selection")
	}
}

func TestProviderChangeRequiresNewConsent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_ = f.engine.Save(ctx, f.owner, "review", "metadata")
	run, err := f.engine.Evaluate(ctx, f.owner, f.ref, true)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewChatProvider("http://127.0.0.1:19999/v1/chat/completions", "different-model", "", false)
	if err != nil {
		t.Fatal(err)
	}
	f.engine.provider = provider
	settings, err := f.engine.Settings(ctx, f.owner)
	if err != nil || settings.Mode != "off" {
		t.Fatal("new destination inherited old consent")
	}
	if err = f.engine.Apply(ctx, f.owner, run.ID, true); !errors.Is(err, ErrChanged) {
		t.Fatal("old consent applied with different provider")
	}
}
