// SPDX-License-Identifier: AGPL-3.0-only
// Package decision implements Mailspace's JEV-style decision gate. A model may
// propose Space ids, never tools, mail sends, permissions or decrypted exports.
package decision

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/privacy"
	"mailspace.local/mailspace/internal/timeline"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

var ErrDisabled = errors.New("classification is disabled")
var ErrChanged = errors.New("classification input, manual choice or Space definition changed")
var ErrInput = errors.New("invalid classification decision")
var ErrProvider = errors.New("classification provider unavailable or invalid response")

type Settings struct {
	Provider string
	Mode     string
	Payload  string
	Revision int64
}
type Definition struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Brief    string `json:"brief"`
	Revision int64  `json:"revision"`
}
type Input struct {
	Spaces  []Definition `json:"spaces"`
	From    string       `json:"from"`
	To      []string     `json:"to"`
	Subject string       `json:"subject"`
	Text    string       `json:"text,omitempty"`
}

func (Input) String() string   { return "<redacted classifier input>" }
func (Input) GoString() string { return "<redacted classifier input>" }

type Proposal struct {
	SpaceIDs   []string `json:"space_ids"`
	Confidence float64  `json:"confidence"`
}
type Provider interface {
	Propose(context.Context, Input) (Proposal, error)
	Name() string
}
type Sources interface {
	Resolve(context.Context, string, mail.SourceRef) (timeline.Resolved, error)
	List(context.Context, string, string) ([]timeline.Record, error)
}
type snapshot struct {
	Version          int
	Ref              mail.SourceRef
	Key              string
	SettingsRevision int64
	Definitions      []Definition
	DefinitionsHash  string
	ManualRevision   int64
	Purpose          string
}
type Run struct {
	ID        string
	State     string
	CreatedAt time.Time
	Proposal  Proposal
	snapshot  snapshot
}
type Service struct {
	db       *sql.DB
	sources  Sources
	groups   *collections.Service
	provider Provider
	busy     sync.Mutex
	calls    atomic.Int64
}

func New(db *sql.DB, sources Sources, groups *collections.Service, provider Provider) *Service {
	return &Service{db: db, sources: sources, groups: groups, provider: provider}
}
func (s *Service) Available() bool { return s != nil && s.provider != nil }
func (s *Service) ProviderName() string {
	if !s.Available() {
		return "Not configured"
	}
	return s.provider.Name()
}
func (s *Service) Settings(ctx context.Context, owner string) (Settings, error) {
	value := Settings{Mode: "off", Payload: "metadata"}
	err := s.db.QueryRowContext(ctx, "SELECT mode,payload,revision,provider FROM decision_settings WHERE owner_id=?", owner).Scan(&value.Mode, &value.Payload, &value.Revision, &value.Provider)
	if errors.Is(err, sql.ErrNoRows) {
		return value, nil
	}
	if err == nil && value.Mode != "off" && value.Provider != s.ProviderName() {
		value.Mode = "off"
	}
	return value, err
}
func (s *Service) Save(ctx context.Context, owner, mode, payload string) error {
	if (mode != "off" && mode != "manual" && mode != "review" && mode != "auto") || (payload != "metadata" && payload != "text") {
		return ErrInput
	}
	if mode != "off" && !s.Available() {
		return ErrDisabled
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO decision_settings(owner_id,mode,payload,provider,revision,updated_at) VALUES(?,?,?,?,1,?) ON CONFLICT(owner_id) DO UPDATE SET mode=excluded.mode,payload=excluded.payload,provider=excluded.provider,revision=revision+1,updated_at=excluded.updated_at`, owner, mode, payload, s.ProviderName(), time.Now().Unix())
	return err
}
func clip(text string, n int) string {
	if len(text) <= n {
		return text
	}
	text = text[:n]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}
func digest(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func (s *Service) prepare(ctx context.Context, owner string, ref mail.SourceRef, purpose string) (snapshot, Input, error) {
	var snap snapshot
	var input Input
	if !s.Available() {
		return snap, input, ErrDisabled
	}
	settings, err := s.Settings(ctx, owner)
	if err != nil {
		return snap, input, err
	}
	if settings.Mode == "off" {
		return snap, input, ErrDisabled
	}
	resolved, err := s.sources.Resolve(ctx, owner, ref)
	if err != nil {
		return snap, input, ErrChanged
	}
	message := resolved.Record
	var archived int
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM message_archives WHERE account_id=? AND account_revision=? AND message_key=?", ref.AccountID, ref.Revision, message.Identity.Key).Scan(&archived); err != nil {
		return snap, input, err
	}
	if archived != 0 {
		return snap, input, ErrChanged
	}
	// Never send encrypted, client-decrypted, pending, unsupported or own mail.
	if message.Preview.Encrypted || message.Own || message.Preview.Status != "ready" {
		return snap, input, ErrInput
	}
	items, err := s.groups.List(ctx, owner, "space")
	if err != nil {
		return snap, input, err
	}
	if len(items) == 0 {
		return snap, input, ErrInput
	}
	var manual int64
	if err = s.db.QueryRowContext(ctx, "SELECT coalesce((SELECT revision FROM message_manual_revisions WHERE account_id=? AND account_revision=? AND message_key=?),0)", ref.AccountID, ref.Revision, message.Identity.Key).Scan(&manual); err != nil {
		return snap, input, err
	}
	if manual != 0 && purpose == "classification" {
		return snap, input, ErrChanged
	}
	snap = snapshot{Version: 1, Ref: ref, Key: message.Identity.Key, SettingsRevision: settings.Revision, DefinitionsHash: digest(items), Purpose: purpose, ManualRevision: manual}
	for _, item := range items {
		snap.Definitions = append(snap.Definitions, Definition{ID: item.ID, Revision: item.Revision})
		input.Spaces = append(input.Spaces, Definition{ID: item.ID, Name: item.Name, Brief: clip(item.Description, 512), Revision: item.Revision})
	}
	input.From = message.Preview.Sender
	input.To = message.Preview.To
	input.Subject = message.Preview.Subject
	if settings.Payload == "text" {
		input.Text = clip(message.Preview.Text, 4096)
	}
	return snap, input, nil
}
func (s *Service) Evaluate(ctx context.Context, owner string, ref mail.SourceRef, dry bool) (Run, error) {
	if !s.busy.TryLock() {
		return Run{}, ErrProvider
	}
	defer s.busy.Unlock()
	purpose := "classification"
	if dry {
		purpose = "dry-run"
	}
	snap, input, err := s.prepare(ctx, owner, ref, purpose)
	if err != nil {
		return Run{}, err
	}
	key := digest([]any{snap.Key, ref.AccountID, ref.Revision, snap.SettingsRevision, snap.DefinitionsHash, purpose, snap.ManualRevision})
	return s.evaluatePrepared(ctx, owner, snap, input, key, dry)
}
func (s *Service) evaluatePrepared(ctx context.Context, owner string, snap snapshot, input Input, key string, dry bool) (Run, error) {
	ref := snap.Ref
	var existing string
	err := s.db.QueryRowContext(ctx, "SELECT id FROM decision_runs WHERE owner_id=? AND cache_key=?", owner, key).Scan(&existing)
	if err == nil {
		return s.Get(ctx, owner, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Run{}, err
	}
	var recent int
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM decision_runs WHERE owner_id=? AND created_at>=?", owner, time.Now().Add(-time.Minute).Unix()).Scan(&recent); err != nil {
		return Run{}, err
	}
	if recent >= 6 {
		return Run{}, ErrProvider
	}
	if s.runCount(ctx, owner) >= 5000 {
		return Run{}, collections.ErrCapacity
	}
	encoded, _ := json.Marshal(snap)
	id := rand.Text()
	result, err := s.db.ExecContext(ctx, "INSERT INTO decision_runs(id,owner_id,account_id,cache_key,snapshot,state,created_at) VALUES(?,?,?,?,?,'pending',?) ON CONFLICT(owner_id,cache_key) DO NOTHING", id, owner, ref.AccountID, key, string(encoded), time.Now().Unix())
	if err != nil {
		return Run{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Run{}, err
	}
	if changed == 0 {
		if err = s.db.QueryRowContext(ctx, "SELECT id FROM decision_runs WHERE owner_id=? AND cache_key=?", owner, key).Scan(&id); err != nil {
			return Run{}, err
		}
		return s.Get(ctx, owner, id)
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	s.calls.Add(1)
	proposal, err := s.provider.Propose(ctx, input)
	if err == nil {
		err = validate(proposal, snap.Definitions)
	}
	if err != nil {
		_, _ = s.db.ExecContext(context.WithoutCancel(ctx), "UPDATE decision_runs SET state='error' WHERE id=? AND owner_id=?", id, owner)
		return Run{}, ErrProvider
	}
	sort.Strings(proposal.SpaceIDs)
	raw, _ := json.Marshal(proposal)
	state := "review"
	if dry {
		state = "dry-run"
	}
	if _, err = s.db.ExecContext(ctx, "UPDATE decision_runs SET state=?,decision=? WHERE id=? AND owner_id=?", state, string(raw), id, owner); err != nil {
		return Run{}, err
	}
	run, err := s.Get(ctx, owner, id)
	if err != nil {
		return run, err
	}
	settings, err := s.Settings(ctx, owner)
	if err != nil {
		return run, err
	}
	if !dry && settings.Mode == "auto" && proposal.Confidence >= 0.8 {
		err = s.Apply(ctx, owner, id, false)
		if err != nil {
			_, _ = s.db.ExecContext(ctx, "UPDATE decision_runs SET state='rejected' WHERE owner_id=? AND id=? AND state!='applied'", owner, id)
		}
		return s.Get(ctx, owner, id)
	}

	return run, nil
}
func validate(p Proposal, definitions []Definition) error {
	if math.IsNaN(p.Confidence) || math.IsInf(p.Confidence, 0) || p.Confidence < 0 || p.Confidence > 1 || len(p.SpaceIDs) > 32 {
		return ErrInput
	}
	allowed := map[string]bool{}
	for _, d := range definitions {
		allowed[d.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range p.SpaceIDs {
		if !allowed[id] || seen[id] {
			return ErrInput
		}
		seen[id] = true
	}
	return nil
}
func (s *Service) Get(ctx context.Context, owner, id string) (Run, error) {
	var run Run
	var snap, decision string
	var created int64
	err := s.db.QueryRowContext(ctx, "SELECT id,state,created_at,snapshot,decision FROM decision_runs WHERE id=? AND owner_id=?", id, owner).Scan(&run.ID, &run.State, &created, &snap, &decision)
	if err != nil {
		return run, ErrChanged
	}
	if json.Unmarshal([]byte(snap), &run.snapshot) != nil {
		return run, ErrInput
	}
	if decision != "" && json.Unmarshal([]byte(decision), &run.Proposal) != nil {
		return run, ErrInput
	}
	run.CreatedAt = time.Unix(created, 0).UTC()
	return run, nil
}
func (s *Service) Runs(ctx context.Context, owner string) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM decision_runs WHERE owner_id=? ORDER BY created_at DESC,id DESC LIMIT 50", owner)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := []Run{}
	for _, id := range ids {
		run, err := s.Get(ctx, owner, id)
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, nil
}

// Replay validates a stored proposal without invoking a model or changing data.
func (s *Service) Replay(ctx context.Context, owner, id string) error {
	return s.gate(ctx, owner, id, false, false)
}
func (s *Service) Apply(ctx context.Context, owner, id string, approval bool) error {
	return s.gate(ctx, owner, id, true, approval)
}
func (s *Service) gate(ctx context.Context, owner, id string, apply, approval bool) error {
	run, err := s.Get(ctx, owner, id)
	if err != nil {
		return err
	}
	if run.State != "review" && run.State != "dry-run" && run.State != "applied" {
		return ErrInput
	}
	snap := run.snapshot
	if snap.Version != 1 {
		return ErrInput
	}
	current, err := s.sources.Resolve(ctx, owner, snap.Ref)
	if err != nil || current.Record.Identity.Key != snap.Key || (current.Record.Preview.Encrypted && snap.Purpose != "client-selection") {
		return ErrChanged
	}
	if apply && snap.Purpose == "client-selection" && !approval {
		return ErrInput
	}
	if err = validate(run.Proposal, snap.Definitions); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int64
	var mode, provider string
	if err = tx.QueryRowContext(ctx, "SELECT revision,mode,provider FROM decision_settings WHERE owner_id=?", owner).Scan(&revision, &mode, &provider); err != nil || revision != snap.SettingsRevision || mode == "off" || provider != s.ProviderName() {
		return ErrChanged
	}
	var accountValid int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM accounts a JOIN mail_connections c ON c.account_id=a.id WHERE a.id=? AND a.owner_id=? AND c.revision=?", snap.Ref.AccountID, owner, snap.Ref.Revision).Scan(&accountValid); err != nil || accountValid != 1 {
		return ErrChanged
	}
	// Definition changes invalidate the entire decision, including removed/new Spaces.
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM message_collections WHERE owner_id=? AND kind='space'", owner).Scan(&count); err != nil || count != len(snap.Definitions) {
		return ErrChanged
	}
	for _, d := range snap.Definitions {
		var revision int64
		if err = tx.QueryRowContext(ctx, "SELECT revision FROM message_collections WHERE id=? AND owner_id=? AND kind='space'", d.ID, owner).Scan(&revision); err != nil || revision != d.Revision {
			return ErrChanged
		}
	}
	var state string
	if err = tx.QueryRowContext(ctx, "SELECT state FROM decision_runs WHERE id=? AND owner_id=?", id, owner).Scan(&state); err != nil {
		return err
	}
	if state == "applied" {
		return nil
	}
	var manual int64
	if err = tx.QueryRowContext(ctx, "SELECT coalesce((SELECT revision FROM message_manual_revisions WHERE account_id=? AND account_revision=? AND message_key=?),0)", snap.Ref.AccountID, snap.Ref.Revision, snap.Key).Scan(&manual); err != nil || manual != snap.ManualRevision {
		return ErrChanged
	}
	if !apply {
		return nil
	}
	// Confidence is a routing threshold, never authority to change permissions.
	if !approval && run.Proposal.Confidence < 0.8 {
		return ErrInput
	}
	if snap.Purpose == "classification" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM message_collection_memberships WHERE account_id=? AND account_revision=? AND message_key=? AND source='model' AND collection_id IN (SELECT id FROM message_collections WHERE owner_id=? AND kind='space')", snap.Ref.AccountID, snap.Ref.Revision, snap.Key, owner); err != nil {
			return err
		}
	}
	var used int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM message_collection_memberships m JOIN message_collections c ON c.id=m.collection_id WHERE c.owner_id=?", owner).Scan(&used); err != nil {
		return err
	}
	if used+len(run.Proposal.SpaceIDs) > 50000 {
		return collections.ErrCapacity
	}
	for _, target := range run.Proposal.SpaceIDs {
		if _, err = tx.ExecContext(ctx, "INSERT INTO message_collection_memberships(collection_id,account_id,account_revision,message_key,source,actor_id,created_at) VALUES(?,?,?,?,'model',?,?) ON CONFLICT(collection_id,message_key) DO NOTHING", target, snap.Ref.AccountID, snap.Ref.Revision, snap.Key, owner, time.Now().Unix()); err != nil {
			return err
		}
	}
	if approval {
		if _, err = tx.ExecContext(ctx, "INSERT INTO message_manual_revisions(account_id,account_revision,message_key,revision,updated_at) VALUES(?,?,?,1,?) ON CONFLICT(account_id,account_revision,message_key) DO UPDATE SET revision=revision+1,updated_at=excluded.updated_at", snap.Ref.AccountID, snap.Ref.Revision, snap.Key, time.Now().Unix()); err != nil {
			return err
		}
	}
	// CAS makes concurrent review/replay idempotent inside this transaction.
	if _, err = tx.ExecContext(ctx, "UPDATE decision_runs SET state='applied' WHERE id=? AND owner_id=?", id, owner); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) Run(ctx context.Context) {
	if !s.Available() {
		return
	}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.Cycle(ctx)
		}
	}
}
func (s *Service) Cycle(ctx context.Context) {
	if !s.Available() {
		return
	}
	rows, err := s.db.QueryContext(ctx, "SELECT owner_id FROM decision_settings WHERE mode IN ('review','auto') ORDER BY updated_at LIMIT 32")
	if err != nil {
		return
	}
	var owners []string
	for rows.Next() {
		var owner string
		if rows.Scan(&owner) != nil {
			rows.Close()
			return
		}
		owners = append(owners, owner)
	}
	rows.Close()
	for _, owner := range owners {
		records, err := s.sources.List(ctx, owner, "")
		if err != nil {
			continue
		}
		processed := 0
		for _, record := range records {
			if ctx.Err() != nil {
				return
			}
			if record.Own || record.Preview.Encrypted || record.Preview.Status != "ready" {
				continue
			}
			// Existing attempts cost no provider call. Scan all bounded cached inputs to
			// find unclassified messages; process at most three new calls per owner/tick.
			before := s.calls.Load()
			_, _ = s.Evaluate(ctx, owner, record.Ref, false)
			if s.calls.Load() > before {
				processed++
			}
			if processed >= 3 {
				break
			}
		}
	}
}
func (s *Service) runCount(ctx context.Context, owner string) int {
	var count int
	_ = s.db.QueryRowContext(ctx, "SELECT count(*) FROM decision_runs WHERE owner_id=?", owner).Scan(&count)
	return count
}

func (r Run) Reference() mail.SourceRef { return r.snapshot.Ref }

// EvaluateSelection is the sole explicit client-decrypted export exception.
// Excerpts are used in memory for one request, never retained in snapshots,
// traces, cache keys, provider error messages, indexes or background jobs.
func (s *Service) EvaluateSelection(ctx context.Context, owner string, ref mail.SourceRef, excerpt, requestID string, consent privacy.ClientClassificationConsent) (Run, error) {
	if !s.Available() || !consent.Allows(s.ProviderName(), len(excerpt)) || !utf8.ValidString(excerpt) || len(requestID) < 20 || len(requestID) > 80 {
		return Run{}, ErrInput
	}
	if !s.busy.TryLock() {
		return Run{}, ErrProvider
	}
	defer s.busy.Unlock()
	settings, err := s.Settings(ctx, owner)
	if err != nil || settings.Mode == "off" {
		return Run{}, ErrDisabled
	}
	resolved, err := s.sources.Resolve(ctx, owner, ref)
	if err != nil {
		return Run{}, ErrChanged
	}
	if !resolved.Record.Preview.Encrypted {
		return Run{}, ErrInput
	}
	items, err := s.groups.List(ctx, owner, "space")
	if err != nil || len(items) == 0 {
		return Run{}, ErrInput
	}
	var manual int64
	if err = s.db.QueryRowContext(ctx, "SELECT coalesce((SELECT revision FROM message_manual_revisions WHERE account_id=? AND account_revision=? AND message_key=?),0)", ref.AccountID, ref.Revision, resolved.Record.Identity.Key).Scan(&manual); err != nil {
		return Run{}, err
	}
	snap := snapshot{Version: 1, Ref: ref, Key: resolved.Record.Identity.Key, SettingsRevision: settings.Revision, DefinitionsHash: digest(items), Purpose: "client-selection", ManualRevision: manual}
	input := Input{Text: excerpt}
	for _, item := range items {
		snap.Definitions = append(snap.Definitions, Definition{ID: item.ID, Revision: item.Revision})
		input.Spaces = append(input.Spaces, Definition{ID: item.ID, Name: item.Name, Brief: clip(item.Description, 512), Revision: item.Revision})
	}
	key := digest([]any{snap.Key, ref.AccountID, ref.Revision, settings.Revision, snap.DefinitionsHash, "client-selection", requestID})
	return s.evaluatePrepared(ctx, owner, snap, input, key, true)
}

func (r Run) MessageKey() string { return r.snapshot.Key }

// PreviewUnassigned is an explicit bounded dry-run, never a bulk auto-apply.
func (s *Service) PreviewUnassigned(ctx context.Context, owner string) (int, error) {
	return s.checkUnassigned(ctx, owner, true)
}
func (s *Service) CheckUnassigned(ctx context.Context, owner string) (int, error) {
	return s.checkUnassigned(ctx, owner, false)
}
func (s *Service) checkUnassigned(ctx context.Context, owner string, preview bool) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	settings, err := s.Settings(ctx, owner)
	if err != nil {
		return 0, err
	}
	if settings.Mode == "off" || !s.Available() {
		return 0, ErrDisabled
	}
	records, err := s.sources.List(ctx, owner, "")
	if err != nil {
		return 0, err
	}
	members, err := s.groups.Memberships(ctx, owner)
	if err != nil {
		return 0, err
	}
	spaces, err := s.groups.List(ctx, owner, "space")
	if err != nil {
		return 0, err
	}
	archived, err := s.groups.Archived(ctx, owner)
	if err != nil {
		return 0, err
	}
	spaceIDs := map[string]bool{}
	for _, space := range spaces {
		spaceIDs[space.ID] = true
	}
	count := 0
	for _, record := range records {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}
		if record.Own || record.Preview.Encrypted || record.Preview.Status != "ready" || archived[record.Identity.Key] {
			continue
		}
		assigned := false
		for _, id := range members[record.Identity.Key] {
			assigned = assigned || spaceIDs[id]
		}
		if assigned {
			continue
		}
		if _, err = s.Evaluate(ctx, owner, record.Ref, preview || settings.Mode == "manual"); err != nil {
			return count, err
		}
		count++
		if count == 3 {
			break
		}
	}
	return count, nil
}
