// SPDX-License-Identifier: AGPL-3.0-only
// Package rules applies reviewed, deterministic, bounded Space rules locally.
// Rules cannot export content, run scripts, send mail or grant permissions.
package rules

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailfilter"
	"mailspace.local/mailspace/internal/timeline"
	netmail "net/mail"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrInput = errors.New("invalid local rule")
var ErrChanged = errors.New("rule, source or manual choice changed")
var ErrLimit = errors.New("local rule capacity reached")

type Sources interface {
	Resolve(context.Context, string, mail.SourceRef) (timeline.Resolved, error)
	List(context.Context, string, string) ([]timeline.Record, error)
}
type Rule struct {
	ID, Kind, Value, SpaceID, Expression string
	compiled                             *mailfilter.Expr
	Enabled                              bool
	Revision                             int64
}

func (Rule) String() string   { return "<redacted local mail rule>" }
func (Rule) GoString() string { return "<redacted local mail rule>" }

type Service struct {
	db      *sql.DB
	sources Sources
	busy    sync.Mutex
}

func New(db *sql.DB, sources Sources) *Service { return &Service{db: db, sources: sources} }
func fields(kind, value string) bool {
	if value == "" || len(value) > 254 || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	switch kind {
	case "sender":
		a, err := netmail.ParseAddress(value)
		return err == nil && a.Address == value && a.Name == ""
	case "domain":
		if !strings.Contains(value, ".") {
			return false
		}
		for _, label := range strings.Split(value, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return false
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
					return false
				}
			}
		}
		return true
	case "subject":
		return len(value) >= 3
	}
	return false
}
func (s *Service) Save(ctx context.Context, owner, id, kind, value, space string, revision int64) (Rule, error) {
	return s.save(ctx, owner, id, kind, value, space, "", revision)
}
func (s *Service) SaveExpression(ctx context.Context, owner, id, query, space string, revision int64) (Rule, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Rule{}, ErrInput
	}
	if _, err := mailfilter.Compile(query); err != nil {
		return Rule{}, ErrInput
	}
	return s.save(ctx, owner, id, "subject", "", space, query, revision)
}
func (s *Service) save(ctx context.Context, owner, id, kind, value, space, expression string, revision int64) (Rule, error) {
	value = strings.TrimSpace(value)
	if expression == "" && !fields(kind, value) {
		return Rule{}, ErrInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Rule{}, err
	}
	defer tx.Rollback()
	var valid int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM message_collections WHERE id=? AND owner_id=? AND kind='space'", space, owner).Scan(&valid); err != nil || valid != 1 {
		return Rule{}, ErrChanged
	}
	if id == "" {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM message_rules WHERE owner_id=?", owner).Scan(&count); err != nil {
			return Rule{}, err
		}
		if count >= 32 {
			return Rule{}, ErrLimit
		}
		id = rand.Text()
		_, err = tx.ExecContext(ctx, "INSERT INTO message_rules(id,owner_id,kind,value,space_id,enabled,revision,created_at,expression) VALUES(?,?,?,?,?,0,1,?,?)", id, owner, kind, value, space, time.Now().Unix(), expression)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, "UPDATE message_rules SET kind=?,value=?,space_id=?,expression=?,enabled=0,revision=revision+1 WHERE id=? AND owner_id=? AND revision=?", kind, value, space, expression, id, owner, revision)
		if err == nil {
			n, _ := result.RowsAffected()
			if n != 1 {
				return Rule{}, ErrChanged
			}
		}
	}
	if err != nil {
		return Rule{}, err
	}
	if err = tx.Commit(); err != nil {
		return Rule{}, err
	}
	return s.Get(ctx, owner, id)
}
func (s *Service) Get(ctx context.Context, owner, id string) (Rule, error) {
	var r Rule
	err := s.db.QueryRowContext(ctx, "SELECT id,kind,value,space_id,enabled,revision,expression FROM message_rules WHERE id=? AND owner_id=?", id, owner).Scan(&r.ID, &r.Kind, &r.Value, &r.SpaceID, &r.Enabled, &r.Revision, &r.Expression)
	if err != nil {
		return r, ErrChanged
	}
	if r.Expression != "" {
		r.compiled, err = mailfilter.Compile(r.Expression)
		if err != nil {
			return Rule{}, ErrInput
		}
	}
	return r, nil
}
func (s *Service) List(ctx context.Context, owner string) ([]Rule, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,kind,value,space_id,enabled,revision,expression FROM message_rules WHERE owner_id=? ORDER BY created_at,id LIMIT 32", owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Rule{}
	for rows.Next() {
		var r Rule
		if err = rows.Scan(&r.ID, &r.Kind, &r.Value, &r.SpaceID, &r.Enabled, &r.Revision, &r.Expression); err != nil {
			return nil, err
		}
		if r.Expression != "" {
			r.compiled, err = mailfilter.Compile(r.Expression)
			if err != nil {
				return nil, ErrInput
			}
		}
		list = append(list, r)
	}
	return list, rows.Err()
}
func (s *Service) Enable(ctx context.Context, owner, id string, revision int64, on bool) error {
	result, err := s.db.ExecContext(ctx, "UPDATE message_rules SET enabled=?,revision=revision+1 WHERE id=? AND owner_id=? AND revision=?", on, id, owner, revision)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrChanged
	}
	return nil
}
func (s *Service) Delete(ctx context.Context, owner, id string, revision int64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM message_rules WHERE id=? AND owner_id=? AND revision=?", id, owner, revision)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrChanged
	}
	return nil
}
func Matches(r Rule, m timeline.Record) bool {
	if m.Own || m.Preview.Encrypted || m.Preview.Status != "ready" {
		return false
	}
	if r.Expression != "" {
		expr := r.compiled
		if expr == nil {
			var err error
			expr, err = mailfilter.Compile(r.Expression)
			if err != nil {
				return false
			}
		}
		return expr.Match(mailfilter.Message{From: m.Preview.Sender, To: m.Preview.To, Subject: m.Preview.Subject, Name: m.Preview.Name, Text: m.Preview.Text, Encrypted: m.Preview.Encrypted})
	}
	switch r.Kind {
	case "sender":
		return strings.EqualFold(m.Preview.Sender, r.Value)
	case "domain":
		at := strings.LastIndexByte(m.Preview.Sender, '@')
		return at >= 0 && strings.EqualFold(m.Preview.Sender[at+1:], r.Value)
	case "subject":
		return strings.Contains(strings.ToLower(m.Preview.Subject), strings.ToLower(r.Value))
	}
	return false
}

type Preview struct {
	Messages      []timeline.Record
	ManualSkipped int
}

func (s *Service) Preview(ctx context.Context, owner, id string) (Preview, error) {
	r, err := s.Get(ctx, owner, id)
	if err != nil {
		return Preview{}, err
	}
	messages, err := s.sources.List(ctx, owner, "")
	if err != nil {
		return Preview{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result := Preview{Messages: []timeline.Record{}}
	for _, m := range messages {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !Matches(r, m) {
			continue
		}
		manual, err := s.manual(ctx, m)
		if err != nil {
			return result, err
		}
		if manual > 0 {
			result.ManualSkipped++
			continue
		}
		if len(result.Messages) < 50 {
			result.Messages = append(result.Messages, m)
		}
	}
	return result, nil
}
func (s *Service) manual(ctx context.Context, m timeline.Record) (int64, error) {
	var value int64
	err := s.db.QueryRowContext(ctx, "SELECT coalesce((SELECT revision FROM message_manual_revisions WHERE account_id=? AND account_revision=? AND message_key=?),0)", m.Ref.AccountID, m.Ref.Revision, m.Identity.Key).Scan(&value)
	return value, err
}

// Process is invoked by existing successful mail sync, never a separate worker.
// No rules means no job. One invocation applies at most 50 new rule decisions.
func (s *Service) Process(ctx context.Context, owner, account string) (int, error) {
	if !s.busy.TryLock() {
		return 0, nil
	}
	defer s.busy.Unlock()
	rules, err := s.List(ctx, owner)
	if err != nil {
		return 0, err
	}
	enabled := []Rule{}
	for _, r := range rules {
		if r.Enabled {
			enabled = append(enabled, r)
		}
	}
	if len(enabled) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	messages, err := s.sources.List(ctx, owner, account)
	if err != nil {
		return 0, err
	}
	applied := 0
	for _, m := range messages {
		if err := ctx.Err(); err != nil {
			return applied, err
		}
		for _, r := range enabled {
			if !Matches(r, m) {
				continue
			}
			changed, err := s.apply(ctx, owner, r, m)
			if errors.Is(err, ErrChanged) {
				continue
			}
			if err != nil {
				return applied, err
			}
			if changed {
				applied++
			}
			if applied >= 50 {
				return applied, nil
			}
		}
	}
	return applied, nil
}
func (s *Service) apply(ctx context.Context, owner string, r Rule, m timeline.Record) (bool, error) {
	current, err := s.sources.Resolve(ctx, owner, m.Ref)
	if err != nil || current.Record.Identity.Key != m.Identity.Key || !Matches(r, current.Record) {
		return false, ErrChanged
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var valid int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM message_rules r JOIN message_collections c ON c.id=r.space_id JOIN accounts a ON a.id=? JOIN mail_connections mc ON mc.account_id=a.id WHERE r.id=? AND r.owner_id=? AND r.enabled=1 AND r.revision=? AND c.kind='space' AND c.owner_id=r.owner_id AND a.owner_id=r.owner_id AND mc.revision=?`, m.Ref.AccountID, r.ID, owner, r.Revision, m.Ref.Revision).Scan(&valid); err != nil || valid != 1 {
		return false, ErrChanged
	}
	var manual int64
	if err = tx.QueryRowContext(ctx, "SELECT coalesce((SELECT revision FROM message_manual_revisions WHERE account_id=? AND account_revision=? AND message_key=?),0)", m.Ref.AccountID, m.Ref.Revision, m.Identity.Key).Scan(&manual); err != nil || manual != 0 {
		return false, ErrChanged
	}
	var seen int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM message_rule_runs WHERE rule_id=? AND rule_revision=? AND account_id=? AND account_revision=? AND message_key=?", r.ID, r.Revision, m.Ref.AccountID, m.Ref.Revision, m.Identity.Key).Scan(&seen); err != nil {
		return false, err
	}
	if seen > 0 {
		return false, nil
	}
	var used int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM message_rule_runs x JOIN message_rules r ON r.id=x.rule_id WHERE r.owner_id=?", owner).Scan(&used); err != nil {
		return false, err
	}
	if used >= 5000 {
		return false, ErrLimit
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM message_collection_memberships m JOIN message_collections c ON c.id=m.collection_id WHERE c.owner_id=?", owner).Scan(&used); err != nil {
		return false, err
	}
	if used >= 50000 {
		return false, ErrLimit
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO message_collection_memberships(collection_id,account_id,account_revision,message_key,source,actor_id,created_at) VALUES(?,?,?,?,'rule',?,?) ON CONFLICT(collection_id,message_key) DO NOTHING", r.SpaceID, m.Ref.AccountID, m.Ref.Revision, m.Identity.Key, owner, time.Now().Unix()); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO message_rule_runs(id,rule_id,rule_revision,account_id,account_revision,message_key,created_at) VALUES(?,?,?,?,?,?,?)", rand.Text(), r.ID, r.Revision, m.Ref.AccountID, m.Ref.Revision, m.Identity.Key, time.Now().Unix()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
