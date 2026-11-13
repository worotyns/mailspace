// SPDX-License-Identifier: AGPL-3.0-only
package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInput = errors.New("invalid profile or workspace input")
var ErrLimit = errors.New("workspace membership limit reached")

type Workspace struct {
	ID      string
	Name    string
	OwnerID string
	Role    string
}
type Member struct {
	UserID string
	Name   string
	Email  string
	Owner  bool
}

func validName(name string, max int) bool {
	return name != "" && len(name) <= max && utf8.ValidString(name) && !strings.ContainsAny(name, "\x00\r\n")
}
func (s *Store) UpdateProfile(ctx context.Context, user, name, zone string) error {
	name = strings.TrimSpace(name)
	if name != "" && !validName(name, 120) {
		return ErrInput
	}
	if zone == "" || zone == "Local" || len(zone) > 64 {
		return ErrInput
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return ErrInput
	}
	result, err := s.db.ExecContext(ctx, "UPDATE users SET display_name=?,timezone=? WHERE id=?", name, zone, user)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) Workspaces(ctx context.Context, user string) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT workspaces.id,name,owner_id FROM workspaces JOIN workspace_members ON workspace_id=workspaces.id WHERE user_id=? ORDER BY created_at,workspaces.id", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Workspace{}
	for rows.Next() {
		var w Workspace
		if err = rows.Scan(&w.ID, &w.Name, &w.OwnerID); err != nil {
			return nil, err
		}
		w.Role = "member"
		if w.OwnerID == user {
			w.Role = "owner"
		}
		list = append(list, w)
	}
	return list, rows.Err()
}
func (s *Store) Workspace(ctx context.Context, user, id string) (Workspace, error) {
	var w Workspace
	err := s.db.QueryRowContext(ctx, "SELECT workspaces.id,name,owner_id FROM workspaces JOIN workspace_members ON workspace_id=workspaces.id WHERE user_id=? AND workspaces.id=?", user, id).Scan(&w.ID, &w.Name, &w.OwnerID)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	w.Role = "member"
	if w.OwnerID == user {
		w.Role = "owner"
	}
	return w, err
}
func (s *Store) ActiveWorkspace(ctx context.Context, user string) (Workspace, error) {
	var w Workspace
	err := s.db.QueryRowContext(ctx, "SELECT workspaces.id,name,owner_id FROM user_preferences JOIN workspaces ON workspaces.id=active_workspace JOIN workspace_members ON workspace_id=workspaces.id AND workspace_members.user_id=user_preferences.user_id WHERE user_preferences.user_id=?", user).Scan(&w.ID, &w.Name, &w.OwnerID)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{Name: "Personal workspace", Role: "personal"}, nil
	}
	w.Role = "member"
	if w.OwnerID == user {
		w.Role = "owner"
	}
	return w, err
}
func audit(ctx context.Context, tx *sql.Tx, workspace, actor, action string, target any) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO workspace_audit(id,workspace_id,actor_id,target_id,action,created_at) VALUES(?,?,?,?,?,?)", rand.Text(), workspace, actor, target, action, time.Now().Unix())
	return err
}
func (s *Store) CreateWorkspace(ctx context.Context, user, name string) (Workspace, error) {
	name = strings.TrimSpace(name)
	if !validName(name, 80) {
		return Workspace{}, ErrInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM workspaces WHERE owner_id=?", user).Scan(&count); err != nil {
		return Workspace{}, err
	}
	if count >= 20 {
		return Workspace{}, ErrLimit
	}
	w := Workspace{ID: rand.Text(), Name: name, OwnerID: user, Role: "owner"}
	if _, err = tx.ExecContext(ctx, "INSERT INTO workspaces(id,name,owner_id,created_at) VALUES(?,?,?,?)", w.ID, w.Name, user, time.Now().Unix()); err != nil {
		return Workspace{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO workspace_members(workspace_id,user_id) VALUES(?,?)", w.ID, user); err != nil {
		return Workspace{}, err
	}
	if err = audit(ctx, tx, w.ID, user, "created", nil); err != nil {
		return Workspace{}, err
	}
	if err = tx.Commit(); err != nil {
		return Workspace{}, err
	}
	return w, nil
}
func (s *Store) SwitchWorkspace(ctx context.Context, user, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if id == "" {
		_, err = tx.ExecContext(ctx, "DELETE FROM user_preferences WHERE user_id=?", user)
	} else {
		var found string
		if err = tx.QueryRowContext(ctx, "SELECT workspace_id FROM workspace_members WHERE workspace_id=? AND user_id=?", id, user).Scan(&found); err != nil {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO user_preferences(user_id,active_workspace) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET active_workspace=excluded.active_workspace", user, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Members(ctx context.Context, user, id string) ([]Member, error) {
	workspace, err := s.Workspace(ctx, user, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT users.id,display_name,email FROM workspace_members JOIN users ON users.id=user_id WHERE workspace_id=? ORDER BY email", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Member{}
	for rows.Next() {
		var m Member
		if err = rows.Scan(&m.UserID, &m.Name, &m.Email); err != nil {
			return nil, err
		}
		m.Owner = m.UserID == workspace.OwnerID
		list = append(list, m)
	}
	return list, rows.Err()
}
func (s *Store) AddMember(ctx context.Context, owner, id, email string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return ErrInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var actual, target string
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT owner_id FROM workspaces WHERE id=?", id).Scan(&actual); err != nil || actual != owner {
		return ErrNotFound
	}
	if err = tx.QueryRowContext(ctx, "SELECT id FROM users WHERE email=?", email).Scan(&target); err != nil {
		return ErrNotFound
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM workspace_members WHERE workspace_id=?", id).Scan(&count); err != nil {
		return err
	}
	if count >= 200 {
		return ErrLimit
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO workspace_members(workspace_id,user_id) VALUES(?,?) ON CONFLICT DO NOTHING", id, target)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n > 0 {
		if err = audit(ctx, tx, id, owner, "member-added", target); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) RemoveMember(ctx context.Context, owner, id, target string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var actual string
	if err = tx.QueryRowContext(ctx, "SELECT owner_id FROM workspaces WHERE id=?", id).Scan(&actual); err != nil || actual != owner || target == owner {
		return ErrNotFound
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM workspace_members WHERE workspace_id=? AND user_id=?", id, target)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if err = audit(ctx, tx, id, owner, "member-removed", target); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) RenameWorkspace(ctx context.Context, owner, id, name string) error {
	name = strings.TrimSpace(name)
	if !validName(name, 80) {
		return ErrInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE workspaces SET name=? WHERE id=? AND owner_id=?", name, id, owner)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrNotFound
	}
	if err = audit(ctx, tx, id, owner, "renamed", nil); err != nil {
		return err
	}
	return tx.Commit()
}
