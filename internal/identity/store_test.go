// SPDX-License-Identifier: AGPL-3.0-only
package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPersistenceAndAccountIsolation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mailspace.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	alice, err := store.CreateUser(ctx, "ALICE@example.test", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.CreateUser(ctx, "bob@example.test", "another sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.CreateAccount(ctx, alice.ID, "Alice private mailbox")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Account(ctx, bob.ID, account.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-user account leak", err)
	}
	if accounts, err := store.Accounts(ctx, bob.ID); err != nil || len(accounts) != 0 {
		t.Fatal("cross-user list leak")
	}
	if _, err = store.CreateAccount(ctx, "missing-user", "unowned"); err == nil {
		t.Fatal("foreign keys disabled")
	}
	token, session, err := store.NewSession(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(token))
	var raw []byte
	if err = store.db.QueryRow("SELECT token_hash FROM sessions WHERE user_id=?", alice.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, hash[:]) || bytes.Equal(raw, []byte(token)) {
		t.Fatal("session token stored as plaintext")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if user, err := store.Authenticate(ctx, "alice@example.test", "a sufficiently long password"); err != nil || user.ID != alice.ID {
		t.Fatal("user lost across restart", err)
	}
	if actual, err := store.Session(ctx, token); err != nil || actual.User.ID != alice.ID || actual.CSRF != session.CSRF {
		t.Fatal("session lost across restart", err)
	}
	if _, err = store.Account(ctx, alice.ID, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Authenticate(ctx, "alice@example.test", "wrong"); !errors.Is(err, ErrCredentials) {
		t.Fatal("wrong password accepted")
	}
	if _, err = store.Authenticate(ctx, "missing@example.test", "wrong"); !errors.Is(err, ErrCredentials) {
		t.Fatal("unknown-user response differs")
	}
	if err = store.RevokeSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Session(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked session survived")
	}
	token, _, err = store.NewSession(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec("UPDATE sessions SET expires_at=?", time.Now().Add(-time.Second).Unix())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Session(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired session accepted")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unsafe DB permissions")
	}
	var mode string
	if err = store.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatal("WAL not enabled")
	}
}
func TestMigrationRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES(7,0)"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if next, err := Open(path); err == nil {
		next.Close()
		t.Fatal("older binary opened newer schema")
	}
}

func TestOfflineBackupRestoreAndPasswordPolicy(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "source.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateUser(ctx, "short@example.test", "short"); err == nil {
		t.Fatal("weak password accepted")
	}
	user, err := store.CreateUser(ctx, "restore@example.test", "a long synthetic restore password")
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.CreateAccount(ctx, user.ID, "restore mailbox")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateUser(ctx, "RESTORE@example.test", "another long synthetic password"); err == nil {
		t.Fatal("duplicate identity accepted")
	}
	var storedHash string
	if err = store.db.QueryRow("SELECT password_hash FROM users WHERE id=?", user.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix([]byte(storedHash), []byte("$argon2id$")) || bytes.Contains([]byte(storedHash), []byte("synthetic restore")) {
		t.Fatal("password stored incorrectly")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(dir, "restored.db")
	if err = os.WriteFile(restoredPath, snapshot, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if actual, err := restored.Authenticate(ctx, user.Email, "a long synthetic restore password"); err != nil || actual.ID != user.ID {
		t.Fatal("restore lost user", err)
	}
	if _, err = restored.Account(ctx, user.ID, account.ID); err != nil {
		t.Fatal("restore lost ownership", err)
	}
}

func TestUpgradeFromIdentityOnlySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(context.Background(), "legacy@example.test", "a synthetic legacy password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec("DROP TABLE message_collection_memberships; DROP TABLE message_manual_revisions; DROP TABLE message_collections; DROP TABLE workspace_audit; DROP TABLE user_preferences; DROP TABLE workspace_members; DROP TABLE workspaces; ALTER TABLE users DROP COLUMN display_name; ALTER TABLE users DROP COLUMN timezone; DROP TABLE outbox; DROP TABLE mail_messages; DROP TABLE mailboxes; DROP TABLE mail_connections; DELETE FROM schema_migrations WHERE version>1"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	upgraded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var actual string
	if err = upgraded.db.QueryRow("SELECT email FROM users WHERE id=?", user.ID).Scan(&actual); err != nil || actual != user.Email {
		t.Fatal("migration lost identity", err)
	}
	var count int
	if err = upgraded.db.QueryRow("SELECT count(*) FROM mail_connections").Scan(&count); err != nil || count != 0 {
		t.Fatal("mail schema migration missing", err)
	}
}
