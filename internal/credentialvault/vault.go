// SPDX-License-Identifier: AGPL-3.0-only
// Package credentialvault encrypts server-side IMAP/SMTP passwords only.
// It must never receive OpenPGP private keys, key passphrases or decrypted mail.
package credentialvault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrInvalid = errors.New("mail credential key or ciphertext invalid")

type Box struct {
	KeyID      string
	Ciphertext []byte
}
type Vault struct {
	dir string
	mu  sync.Mutex
}

func Open(dir string) (*Vault, error)         { return open(dir, true) }
func OpenExisting(dir string) (*Vault, error) { return open(dir, false) }
func open(dir string, create bool) (*Vault, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if create {
		if err = os.MkdirAll(absolute, 0700); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("credential key directory must be a real directory with permissions 0700")
	}
	v := &Vault{dir: absolute}
	if _, err = v.active(); errors.Is(err, os.ErrNotExist) {
		if !create {
			return nil, ErrInvalid
		}
		files, e := filepath.Glob(filepath.Join(absolute, "*.key"))
		if e != nil {
			return nil, e
		}
		if len(files) > 0 {
			return nil, errors.New("credential active-key marker missing; restore the complete keyring backup")
		}
		if _, err = v.Rotate(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return v, nil
}
func validID(id string) bool {
	if len(id) != 32 || id != strings.ToLower(id) {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
func (v *Vault) key(id string) ([]byte, error) {
	if !validID(id) {
		return nil, ErrInvalid
	}
	path := filepath.Join(v.dir, id+".key")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != 32 {
		return nil, ErrInvalid
	}
	key, err := os.ReadFile(path)
	if err != nil || len(key) != 32 {
		return nil, ErrInvalid
	}
	digest := sha256.Sum256(key)
	if hex.EncodeToString(digest[:16]) != id {
		clear(key)
		return nil, ErrInvalid
	}
	return key, nil
}
func (v *Vault) active() (string, error) {
	path := filepath.Join(v.dir, "active")
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 32 || info.Size() > 34 {
		return "", ErrInvalid
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(value))
	key, err := v.key(id)
	clear(key)
	if err != nil {
		return "", err
	}
	return id, nil
}
func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Rotate durably creates a new key and activates it, retaining every old key.
// Old boxes remain readable. Database re-encryption is a separate transaction;
// a crash/rollback cannot lose access to boxes using either key.
func (v *Vault) Rotate() (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	defer clear(key)
	digest := sha256.Sum256(key)
	id := hex.EncodeToString(digest[:16])
	path := filepath.Join(v.dir, id+".key")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	if _, err = file.Write(key); err != nil {
		file.Close()
		return "", err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	if err = syncDirectory(v.dir); err != nil {
		return "", err
	}
	marker, err := os.CreateTemp(v.dir, ".active-*")
	if err != nil {
		return "", err
	}
	temporary := marker.Name()
	defer os.Remove(temporary)
	if _, err = marker.WriteString(id + "\n"); err != nil {
		marker.Close()
		return "", err
	}
	if err = marker.Sync(); err != nil {
		marker.Close()
		return "", err
	}
	if err = marker.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(temporary, filepath.Join(v.dir, "active")); err != nil {
		return "", err
	}
	if err = syncDirectory(v.dir); err != nil {
		return "", err
	}
	return id, nil
}
func (v *Vault) Seal(plaintext, aad []byte) (Box, error) {
	id, err := v.active()
	if err != nil {
		return Box{}, err
	}
	key, err := v.key(id)
	if err != nil {
		return Box{}, err
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return Box{}, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return Box{}, err
	}
	return Box{KeyID: id, Ciphertext: aead.Seal(nil, nil, plaintext, aad)}, nil
}
func (v *Vault) Unseal(box Box, aad []byte) ([]byte, error) {
	key, err := v.key(box.KeyID)
	if err != nil {
		return nil, ErrInvalid
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalid
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, ErrInvalid
	}
	value, err := aead.Open(nil, nil, box.Ciphertext, aad)
	if err != nil {
		return nil, ErrInvalid
	}
	return value, nil
}
