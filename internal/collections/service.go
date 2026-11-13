// SPDX-License-Identifier: AGPL-3.0-only
// Package collections owns private message-level Spaces/labels. Classification
// selects references; it does not copy mail, move IMAP messages or grant access.
package collections

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"mailspace.local/mailspace/internal/mail"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInput = errors.New("invalid collection input")
var ErrNotFound = errors.New("collection or message not found")
var ErrCapacity = errors.New("collection capacity reached")
var ErrChanged = errors.New("source or collection changed")

type Item struct {
	ID          string
	Kind        string
	Name        string
	Description string
	Color       string
	Revision    int64
}
type Resolver interface {
	Identity(context.Context, string, mail.SourceRef) (mail.MessageIdentity, error)
}
type Service struct {
	db       *sql.DB
	resolver Resolver
}

func New(db *sql.DB, r Resolver) *Service { return &Service{db: db, resolver: r} }
func kindOK(kind string) bool             { return kind == "space" || kind == "label" }
func fields(kind, name, description, color string) error {
	if !kindOK(kind) || !utf8.ValidString(name) || !utf8.ValidString(description) || strings.TrimSpace(name) == "" || len(name) > 200 || utf8.RuneCountInString(name) > 80 || len(description) > 8000 || strings.ContainsAny(name, "\r\n\x00") || strings.ContainsRune(description, '\x00') || (kind == "label" && description != "") {
		return ErrInput
	}
	switch color {
	case "lilac", "blue", "green", "peach", "rose", "amber", "red", "pink", "fuchsia", "purple", "indigo", "cyan", "teal", "lime", "yellow", "slate":
		return nil
	}
	return ErrInput
}
func (s *Service) List(ctx context.Context, owner, kind string) ([]Item, error) {
	if !kindOK(kind) {
		return nil, ErrInput
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,kind,name,description,coalesce(palette_color,color),revision FROM message_collections WHERE owner_id=? AND kind=? ORDER BY created_at,id", owner, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Item{}
	for rows.Next() {
		var item Item
		if err = rows.Scan(&item.ID, &item.Kind, &item.Name, &item.Description, &item.Color, &item.Revision); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Service) Get(ctx context.Context, owner, kind, id string) (Item, error) {
	var item Item
	err := s.db.QueryRowContext(ctx, "SELECT id,kind,name,description,coalesce(palette_color,color),revision FROM message_collections WHERE id=? AND owner_id=? AND kind=?", id, owner, kind).Scan(&item.ID, &item.Kind, &item.Name, &item.Description, &item.Color, &item.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	return item, err
}
func (s *Service) Create(ctx context.Context, owner, kind, name, description, color string) (Item, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if err := fields(kind, name, description, color); err != nil {
		return Item{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM message_collections WHERE owner_id=? AND kind=?", owner, kind).Scan(&count); err != nil {
		return Item{}, err
	}
	limit := 32
	if kind == "label" {
		limit = 64
	}
	if count >= limit {
		return Item{}, ErrCapacity
	}
	id := rand.Text()
	now := time.Now().Unix()
	_, err = tx.ExecContext(ctx, "INSERT INTO message_collections(id,owner_id,kind,name,description,color,palette_color,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)", id, owner, kind, name, description, legacyColor(color), color, now, now)
	if err != nil {
		return Item{}, ErrInput
	}
	if err = tx.Commit(); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, owner, kind, id)
}
func (s *Service) Update(ctx context.Context, owner, kind, id, name, description, color string, revision int64) (Item, error) {
	if _, err := s.Get(ctx, owner, kind, id); err != nil {
		return Item{}, err
	}
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if err := fields(kind, name, description, color); err != nil {
		return Item{}, err
	}
	result, err := s.db.ExecContext(ctx, "UPDATE message_collections SET name=?,description=?,color=?,palette_color=?,revision=revision+1,updated_at=? WHERE id=? AND owner_id=? AND kind=? AND revision=?", name, description, legacyColor(color), color, time.Now().Unix(), id, owner, kind, revision)
	if err != nil {
		return Item{}, ErrInput
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Item{}, err
	}
	if n != 1 {
		return Item{}, ErrChanged
	}
	return s.Get(ctx, owner, kind, id)
}
func (s *Service) Remove(ctx context.Context, owner, kind, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM message_collections WHERE id=? AND owner_id=? AND kind=?", id, owner, kind)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

// Assign re-authorizes both ends on every mutation. A caller supplies only a
// physical reference; a model/client cannot invent a logical source identity.
func (s *Service) Assign(ctx context.Context, owner, kind, id string, ref mail.SourceRef, selected bool) error {
	if _, err := s.Get(ctx, owner, kind, id); err != nil {
		return err
	}
	if s.resolver == nil {
		return ErrNotFound
	}
	identity, err := s.resolver.Identity(ctx, owner, ref)
	if err != nil {
		return ErrNotFound
	}
	hash, hashError := hex.DecodeString(identity.Key)
	if identity.AccountID != ref.AccountID || len(hash) != 32 || hashError != nil || identity.Revision < 1 || identity.Revision != ref.Revision {
		return ErrInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var valid int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM message_collections c JOIN accounts a ON a.owner_id=c.owner_id JOIN mail_connections mc ON mc.account_id=a.id WHERE c.id=? AND c.owner_id=? AND c.kind=? AND a.id=? AND mc.revision=?`, id, owner, kind, identity.AccountID, identity.Revision).Scan(&valid)
	if err != nil {
		return err
	}
	if valid != 1 {
		return ErrChanged
	}
	var result sql.Result
	if selected {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM message_collection_memberships WHERE collection_id IN (SELECT id FROM message_collections WHERE owner_id=?)", owner).Scan(&count); err != nil {
			return err
		}
		if count >= 50000 {
			var present int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM message_collection_memberships WHERE collection_id=? AND message_key=?", id, identity.Key).Scan(&present); err != nil {
				return err
			}
			if present == 0 {
				return ErrCapacity
			}
		}
		result, err = tx.ExecContext(ctx, "INSERT INTO message_collection_memberships(collection_id,account_id,account_revision,message_key,source,actor_id,created_at) VALUES(?,?,?,?,'manual',?,?) ON CONFLICT(collection_id,message_key) DO UPDATE SET source='manual',actor_id=excluded.actor_id WHERE message_collection_memberships.source!='manual'", id, identity.AccountID, identity.Revision, identity.Key, owner, time.Now().Unix())
	} else {
		result, err = tx.ExecContext(ctx, "DELETE FROM message_collection_memberships WHERE collection_id=? AND message_key=? AND account_id=? AND account_revision=?", id, identity.Key, identity.AccountID, identity.Revision)
	}
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 && kind == "space" {
		_, err = tx.ExecContext(ctx, `INSERT INTO message_manual_revisions(account_id,account_revision,message_key,revision,updated_at) VALUES(?,?,?,1,?) ON CONFLICT(account_id,account_revision,message_key) DO UPDATE SET revision=revision+1,updated_at=excluded.updated_at`, identity.AccountID, identity.Revision, identity.Key, time.Now().Unix())
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Memberships contains only current owner-authorized reference keys. Callers
// intersect with their live source feed; vanished sources never become visible.
func (s *Service) Memberships(ctx context.Context, owner string) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.message_key,m.collection_id FROM message_collection_memberships m JOIN message_collections c ON c.id=m.collection_id JOIN accounts a ON a.id=m.account_id JOIN mail_connections mc ON mc.account_id=a.id WHERE c.owner_id=? AND a.owner_id=? AND mc.revision=m.account_revision ORDER BY c.created_at,c.id`, owner, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]string{}
	for rows.Next() {
		var key, id string
		if err = rows.Scan(&key, &id); err != nil {
			return nil, err
		}
		result[key] = append(result[key], id)
	}
	return result, rows.Err()
}

func legacyColor(color string) string {
	switch color {
	case "lilac", "blue", "green", "peach", "rose", "amber":
		return color
	}
	return "lilac"
}
