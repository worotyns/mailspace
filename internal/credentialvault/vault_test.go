// SPDX-License-Identifier: AGPL-3.0-only
package credentialvault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialBindingRotationAndRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	vault, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("synthetic IMAP app password")
	aad := []byte("alice/account-1/imap/v1")
	first, err := vault.Seal(secret, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(first.Ciphertext, secret) {
		t.Fatal("plaintext in ciphertext")
	}
	second, err := vault.Seal(secret, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Ciphertext, second.Ciphertext) {
		t.Fatal("nonce reused")
	}
	if _, err = vault.Unseal(first, []byte("bob/account-1/imap/v1")); err == nil {
		t.Fatal("cross-owner box swap accepted")
	}
	if _, err = vault.Unseal(first, []byte("alice/account-2/imap/v1")); err == nil {
		t.Fatal("cross-account box swap accepted")
	}
	changed := Box{KeyID: first.KeyID, Ciphertext: append([]byte{}, first.Ciphertext...)}
	changed.Ciphertext[len(changed.Ciphertext)-1] ^= 1
	if _, err = vault.Unseal(changed, aad); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	id, err := vault.Rotate()
	if err != nil || id == first.KeyID {
		t.Fatal("rotation failed", err)
	}
	fresh, err := vault.Seal(secret, aad)
	if err != nil || fresh.KeyID != id {
		t.Fatal("new key not active")
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, box := range []Box{first, fresh} {
		plain, err := reopened.Unseal(box, aad)
		if err != nil || !bytes.Equal(plain, secret) {
			t.Fatal("rotation/restart lost credentials", err)
		}
	}
	if err = os.Remove(filepath.Join(dir, first.KeyID+".key")); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Unseal(first, aad); err == nil {
		t.Fatal("missing key silently replaced")
	}
}
func TestKeyPermissionsAndTraversal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	vault, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	box, err := vault.Seal([]byte("fixture"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(dir, box.KeyID+".key"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = vault.Unseal(box, nil); err == nil {
		t.Fatal("insecure key permissions accepted")
	}
	box.KeyID = "../../outside"
	if _, err = vault.Unseal(box, nil); err == nil {
		t.Fatal("key path traversal")
	}
}
