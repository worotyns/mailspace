// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"mailspace.local/mailspace/internal/identity"
)

func TestOperatorResetAuthenticatorRetainsPasswordAndOtherUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.db")
	t.Setenv("MAILSPACE_DB", path)
	store, err := identity.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	const password = "synthetic operator recovery password"
	alice, err := store.CreateUser(ctx, "alice@example.test", password)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.CreateUser(ctx, "bob@example.test", password)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.NewSession(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	bobToken, _, err := store.NewSession(ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := store.BeginMFAEnrollment(ctx, alice.ID, token, password)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(enrollment.Secret, time.Now())
	active, err := store.ConfirmMFAEnrollment(ctx, alice.ID, token, code)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.PasswordLogin(ctx, alice.Email, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := resetAuthenticator(alice.Email); err != nil {
		t.Fatal(err)
	}
	status, err := store.MFAStatus(ctx, alice.ID)
	if err != nil || status.Enabled || status.RecoveryRemaining != 0 {
		t.Fatal("operator reset did not remove factor")
	}
	if _, err := store.Session(ctx, active.Token); !errors.Is(err, identity.ErrNotFound) {
		t.Fatal("operator reset retained a session")
	}
	if _, err := store.LoginChallenge(ctx, pending.Challenge); !errors.Is(err, identity.ErrNotFound) {
		t.Fatal("operator reset retained pending sign-in")
	}
	if _, err := store.Session(ctx, bobToken); err != nil {
		t.Fatal("operator reset affected another user")
	}
	login, err := store.PasswordLogin(ctx, alice.Email, password)
	if err != nil || login.Token == "" || login.Challenge != "" {
		t.Fatal("reset changed password or retained factor")
	}
	if err := resetAuthenticator("missing@example.test"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatal("unknown operator target silently succeeded")
	}
}
