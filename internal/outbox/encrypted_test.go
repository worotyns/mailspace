// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"bytes"
	"context"
	"errors"
	"mailspace.local/mailspace/internal/mailsync"
	"os"
	"strings"
	"testing"
)

func encryptedFixture(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/encrypted.eml")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func TestEncryptedMIMEBoundaryAndNoPlaintextFallback(t *testing.T) {
	d := Draft{To: "recipient@example.test", BCC: "hidden@example.test", Subject: "Visible metadata", EncryptedMIME: encryptedFixture(t)}
	raw, to, err := Build("owner@example.test", d, &mailsync.Preview{Encrypted: true, MessageID: "parent@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(to) != 2 || !bytes.Contains(raw, []byte("From: <owner@example.test>")) || bytes.Contains(raw, []byte("Bcc:")) || bytes.Contains(raw, []byte("hidden@example.test")) || !bytes.Contains(raw, []byte("In-Reply-To: <parent@example.test>")) {
		t.Fatal("envelope/header boundary failed")
	}
	preview := mailsync.Parse(raw)
	if !preview.Encrypted || preview.Text != "" {
		t.Fatal("ciphertext produced plaintext preview")
	}
	for name, mutate := range map[string]func(*Draft){
		"mixed plaintext":   func(d *Draft) { d.Body = "Private canary" },
		"plaintext consent": func(d *Draft) { d.AllowPlaintext = true },
		"header injection":  func(d *Draft) { d.EncryptedMIME = "From: attacker@example.test\r\n" + d.EncryptedMIME },
		"bad protocol": func(d *Draft) {
			d.EncryptedMIME = strings.ReplaceAll(d.EncryptedMIME, "application/pgp-encrypted", "text/plain")
		},
		"readable fake armor": func(d *Draft) {
			d.EncryptedMIME = strings.ReplaceAll(d.EncryptedMIME, "-----BEGIN PGP MESSAGE-----", "Not encrypted")
		},
		"non CRLF": func(d *Draft) { d.EncryptedMIME = strings.ReplaceAll(d.EncryptedMIME, "\r\n", "\n") },
		"extra plaintext part": func(d *Draft) {
			d.EncryptedMIME = strings.Replace(d.EncryptedMIME, "Content-Type: application/octet-stream", "Content-Type: text/plain", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := d
			mutate(&copy)
			if _, _, err := Build("owner@example.test", copy, nil); !errors.Is(err, ErrInput) {
				t.Fatal("invalid encrypted entity accepted", err)
			}
		})
	}
	// Unknown preambles/epilogues and part headers are never forwarded.
	d.EncryptedMIME = strings.Replace(d.EncryptedMIME, "\r\n\r\n--", "\r\n\r\nPRIVATE_PREAMBLE\r\n--", 1) + "PRIVATE_EPILOGUE"
	d.EncryptedMIME += "\r\n"
	raw, _, err = Build("owner@example.test", d, nil)
	if err != nil || bytes.Contains(raw, []byte("PRIVATE_")) {
		t.Fatal("unencrypted MIME decoration leaked", err)
	}
}
func TestEncryptedOutboxUsesExistingOwnerAndIdempotencyGuards(t *testing.T) {
	s, _, db, sender, owner, bob, account := setup(t)
	ctx := context.Background()
	d := Draft{To: "recipient@example.test", EncryptedMIME: encryptedFixture(t)}
	r, err := s.Queue(ctx, owner, account, nonce, d)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.Queue(ctx, owner, account, nonce, d)
	if err != nil || replay.ID != r.ID {
		t.Fatal("replay changed encrypted request")
	}
	if _, err = s.Queue(ctx, bob, account, nonce, d); err == nil {
		t.Fatal("foreign queue succeeded")
	}
	if err = s.Dispatch(ctx, bob, r.ID); err == nil {
		t.Fatal("foreign dispatch succeeded")
	}
	if err = s.Dispatch(ctx, owner, r.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Dispatch(ctx, owner, r.ID); err != nil {
		t.Fatal(err)
	}
	if sender.calls.Load() != 1 || sender.copies.Load() != 1 {
		t.Fatal("ciphertext was resent")
	}
	var raw []byte
	if err = db.Database().QueryRow("SELECT raw FROM outbox WHERE id=?", r.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !mailsync.Parse(raw).Encrypted || bytes.Contains(raw, []byte("Synthetic encrypted fixture")) {
		t.Fatal("outbox stored plaintext")
	}
}
