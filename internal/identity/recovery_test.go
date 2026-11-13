// SPDX-License-Identifier: AGPL-3.0-only
package identity

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestOperatorPasswordRecoveryRevokesOnlyTargetSessionsAtomically(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	alice, err := store.CreateUser(ctx, "alice@example.test", "Synthetic original password")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.CreateUser(ctx, "bob@example.test", "Synthetic other password")
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := store.NewSession(ctx, alice.ID)
	second, _, _ := store.NewSession(ctx, bob.ID)
	if err = store.ResetPassword(ctx, alice.Email, "weak"); err == nil {
		t.Fatal("weak recovery password accepted")
	}
	if _, err = store.Session(ctx, first); err != nil {
		t.Fatal("failed reset revoked session")
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fail_recovery BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'synthetic revocation failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = store.ResetPassword(ctx, alice.Email, "Synthetic replacement password"); err == nil {
		t.Fatal("revocation failure did not abort reset")
	}
	if _, err = store.Authenticate(ctx, alice.Email, "Synthetic original password"); err != nil {
		t.Fatal("reset did not roll back")
	}
	if _, err = store.db.Exec("DROP TRIGGER fail_recovery"); err != nil {
		t.Fatal(err)
	}
	if err = store.ResetPassword(ctx, "ALICE@example.test", "Synthetic replacement password"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Authenticate(ctx, alice.Email, "Synthetic original password"); err == nil {
		t.Fatal("old password accepted")
	}
	if _, err = store.Authenticate(ctx, alice.Email, "Synthetic replacement password"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Session(ctx, first); !errors.Is(err, ErrNotFound) {
		t.Fatal("target session remains")
	}
	if _, err = store.Session(ctx, second); err != nil {
		t.Fatal("other user's session revoked")
	}
	if err = store.RevokeUserSessions(ctx, bob.Email); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Session(ctx, second); !errors.Is(err, ErrNotFound) {
		t.Fatal("operator revocation failed")
	}
	if err = store.RevokeUserSessions(ctx, "absent@example.test"); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing user accepted")
	}
}
