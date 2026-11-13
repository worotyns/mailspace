// SPDX-License-Identifier: AGPL-3.0-only
package identity

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestWorkspaceMembershipDoesNotGrantAccountOwnership(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "identity.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	alice, err := s.CreateUser(ctx, "alice@example.test", "synthetic password for alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.CreateUser(ctx, "bob@example.test", "synthetic password for bob")
	if err != nil {
		t.Fatal(err)
	}
	charlie, err := s.CreateUser(ctx, "charlie@example.test", "synthetic password for charlie")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateAccount(ctx, alice.ID, "Private account")
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWorkspace(ctx, alice.ID, "Engineering")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SwitchWorkspace(ctx, bob.ID, w.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("outsider switched into workspace")
	}
	if _, err = s.Members(ctx, bob.ID, w.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("outsider read membership")
	}
	if err = s.AddMember(ctx, alice.ID, w.ID, bob.Email); err != nil {
		t.Fatal(err)
	}
	if err = s.SwitchWorkspace(ctx, bob.ID, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Account(ctx, bob.ID, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("membership granted private mail")
	}
	if err = s.AddMember(ctx, bob.ID, w.ID, charlie.Email); !errors.Is(err, ErrNotFound) {
		t.Fatal("member granted another membership")
	}
	if err = s.RenameWorkspace(ctx, bob.ID, w.ID, "Takeover"); !errors.Is(err, ErrNotFound) {
		t.Fatal("member renamed workspace")
	}
	if err = s.RemoveMember(ctx, alice.ID, w.ID, alice.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("owner could remove itself")
	}
	if err = s.UpdateProfile(ctx, bob.ID, "<script>Bob</script>", "Europe/Warsaw"); err != nil {
		t.Fatal(err)
	}
	token, session, err := s.NewSession(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.User.Name != "<script>Bob</script>" || session.User.Timezone != "Europe/Warsaw" {
		t.Fatal("profile not present in session")
	}
	if err = s.RemoveMember(ctx, alice.ID, w.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Workspace(ctx, bob.ID, w.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked membership retained access")
	}
	active, err := s.ActiveWorkspace(ctx, bob.ID)
	if err != nil || active.ID != "" {
		t.Fatal("revoked active workspace did not fall back to personal")
	}
	if _, err = s.Session(ctx, token); err != nil {
		t.Fatal("workspace removal incorrectly revoked personal login")
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM workspace_audit WHERE workspace_id=?", w.ID).Scan(&count); err != nil || count != 3 {
		t.Fatal("membership audit missing", count, err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.Workspaces(ctx, alice.ID)
	if err != nil || len(list) != 1 {
		t.Fatal("workspace did not survive restart")
	}
	if err = s.UpdateProfile(ctx, bob.ID, "Bob", "../../etc/passwd"); !errors.Is(err, ErrInput) {
		t.Fatal("invalid timezone accepted")
	}
}
