// SPDX-License-Identifier: AGPL-3.0-only
package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mailnet"
	"path/filepath"
	"strings"
	"testing"
)

type fakeChecker struct {
	calls int
	fail  bool
}

func (f *fakeChecker) CheckIMAP(context.Context, mailnet.Endpoint, mailnet.Login) error {
	f.calls++
	if f.fail {
		return errors.New("fixture connection unavailable")
	}
	return nil
}
func (f *fakeChecker) CheckSMTP(context.Context, mailnet.Endpoint, mailnet.Login) error {
	f.calls++
	return nil
}
func fixture() Input {
	return Input{Name: "Private mail", Address: "alice@example.test", IMAP: mailnet.Endpoint{Host: "imap.example.test", Port: 993, TLS: "tls"}, SMTP: mailnet.Endpoint{Host: "smtp.example.test", Port: 465, TLS: "tls"}, IMAPLogin: mailnet.Login{Username: "private-login-identifier", Password: "never-store-this-password-plaintext"}, SMTPLogin: mailnet.Login{Username: "private-smtp-login", Password: "another-private-provider-password"}}
}
func TestEncryptedAccountLifecycleAndIsolation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := identity.Open(filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	alice, err := store.CreateUser(ctx, "alice@example.test", "synthetic app login password")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.CreateUser(ctx, "bob@example.test", "another synthetic login password")
	if err != nil {
		t.Fatal(err)
	}
	checker := &fakeChecker{}
	keyDir := filepath.Join(dir, "credential-keys")
	service, err := Open(store.Database(), keyDir, checker)
	if err != nil {
		t.Fatal(err)
	}
	input := fixture()
	config, err := service.Save(ctx, alice.ID, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if config.Revision != 1 {
		t.Fatal("save omitted committed configuration revision", config.Revision)
	}
	before := checker.calls
	if _, err = service.Save(ctx, bob.ID, config.ID, input); !errors.Is(err, ErrNotFound) || checker.calls != before {
		t.Fatal("foreign update touched network or data", err)
	}
	if err = service.Check(ctx, bob.ID, config.ID); !errors.Is(err, ErrNotFound) || checker.calls != before {
		t.Fatal("foreign credentials read")
	}
	var rawConfig string
	var encrypted []byte
	if err = store.Database().QueryRow("SELECT config,imap_secret FROM mail_connections WHERE account_id=?", config.ID).Scan(&rawConfig, &encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rawConfig, input.IMAPLogin.Username) || strings.Contains(string(encrypted), input.IMAPLogin.Password) {
		t.Fatal("plaintext credentials in database")
	}
	public, _ := json.Marshal(config)
	logged := fmt.Sprintf("%+v %#v", input, input)
	if strings.Contains(string(public), input.IMAPLogin.Password) || strings.Contains(logged, input.IMAPLogin.Password) {
		t.Fatal("credentials exposed by serialization/formatting")
	}
	row, err := service.load(ctx, alice.ID, config.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.unseal(row.imap, bob.ID, config.ID, "imap"); err == nil {
		t.Fatal("cross-owner ciphertext swap")
	}
	oldKey := row.imap.KeyID
	newKey, err := service.RotateKey(ctx)
	if err != nil || newKey == oldKey {
		t.Fatal("credential key rotation failed", err)
	}
	after, err := service.load(ctx, alice.ID, config.ID)
	if err != nil || after.imap.KeyID != newKey {
		t.Fatal("rows not re-encrypted", err)
	}
	reopened, err := Open(store.Database(), keyDir, checker)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.Check(ctx, alice.ID, config.ID); err != nil {
		t.Fatal("restart after rotation lost credentials", err)
	}
	input.IMAPLogin.Password = "updated mail provider password"
	if _, err = reopened.Save(ctx, alice.ID, config.ID, input); err != nil {
		t.Fatal("credential replacement failed", err)
	}
	checker.fail = true
	if _, err = reopened.Save(ctx, alice.ID, "", fixture()); err == nil {
		t.Fatal("failed connection was saved")
	}
	accounts, err := store.Accounts(ctx, alice.ID)
	if err != nil || len(accounts) != 1 {
		t.Fatal("failed probe left partial account")
	}
	missing := filepath.Join(dir, "missing-keyring")
	if _, err = Open(store.Database(), missing, checker); err == nil {
		t.Fatal("existing encrypted accounts silently got new keyring")
	}
}

func TestFailedRotationRetainsOldKeysAndRollsBackRows(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := identity.Open(filepath.Join(dir, "rotation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	user, err := store.CreateUser(ctx, "rotate@example.test", "synthetic key rotation password")
	if err != nil {
		t.Fatal(err)
	}
	checker := &fakeChecker{}
	service, err := Open(store.Database(), filepath.Join(dir, "keys"), checker)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Save(ctx, user.ID, "", fixture())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Save(ctx, user.ID, "", fixture())
	if err != nil {
		t.Fatal(err)
	}
	low, high := first.ID, second.ID
	if low > high {
		low, high = high, low
	}
	before, err := service.load(ctx, user.ID, low)
	if err != nil {
		t.Fatal(err)
	}
	original, err := service.load(ctx, user.ID, high)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := append([]byte{}, original.imap.Ciphertext...)
	corrupted[len(corrupted)-1] ^= 1
	if _, err = store.Database().Exec("UPDATE mail_connections SET imap_secret=? WHERE account_id=?", corrupted, high); err != nil {
		t.Fatal(err)
	}
	if _, err = service.RotateKey(ctx); err == nil {
		t.Fatal("corrupt row did not abort rotation")
	}
	after, err := service.load(ctx, user.ID, low)
	if err != nil || after.imap.KeyID != before.imap.KeyID {
		t.Fatal("failed rotation committed partial rows", err)
	}
	if _, err = store.Database().Exec("UPDATE mail_connections SET imap_secret=? WHERE account_id=?", original.imap.Ciphertext, high); err != nil {
		t.Fatal(err)
	}
	if err = service.Check(ctx, user.ID, low); err != nil {
		t.Fatal("failed rotation lost the old key", err)
	}
	if err = service.Check(ctx, user.ID, high); err != nil {
		t.Fatal("restored row is unreadable", err)
	}
	if _, err = service.RotateKey(ctx); err != nil {
		t.Fatal("rotation retry failed", err)
	}
}
