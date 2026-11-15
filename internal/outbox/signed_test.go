// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"mailspace.local/mailspace/internal/mailnet"
	"mailspace.local/mailspace/internal/mailsync"
)

func signedDraft(t *testing.T) Draft {
	t.Helper()
	fixture, err := os.ReadFile("testdata/signed.eml")
	if err != nil {
		t.Fatal(err)
	}
	return Draft{To: "recipient@example.test", BCC: "hidden@example.test", Subject: "Signed authored fixture", SignedMIME: string(fixture), Provenance: "authored", AllowPlaintext: true}
}

func TestSignedTransportPreservesExactEntityAndAuthoritativeEnvelope(t *testing.T) {
	d := signedDraft(t)
	raw, recipients, err := Build("owner@example.test", d, &mailsync.Preview{MessageID: "original@example.test", References: []string{"ancestor@example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(raw, []byte(d.SignedMIME)) || !bytes.Contains(raw, []byte("From: <owner@example.test>\r\n")) || !bytes.Contains(raw, []byte("In-Reply-To: <original@example.test>\r\n")) || bytes.Contains(raw, []byte("hidden@example.test")) || len(recipients) != 2 {
		t.Fatal("signed bytes, original references or authoritative envelope lost")
	}
	if _, _, err = Build("owner@example.test", d, &mailsync.Preview{Encrypted: true}); !errors.Is(err, ErrEncryption) {
		t.Fatal("encrypted parent downgraded", err)
	}
	for name, mutate := range map[string]func(*Draft){
		"no consent":           func(d *Draft) { d.AllowPlaintext = false },
		"missing provenance":   func(d *Draft) { d.Provenance = "" },
		"decrypted provenance": func(d *Draft) { d.Provenance = "client-decrypted" },
		"unknown provenance":   func(d *Draft) { d.Provenance = "future" },
		"body fallback":        func(d *Draft) { d.Body = "canary" },
		"encrypted fallback":   func(d *Draft) { d.EncryptedMIME = encryptedFixture(t) },
		"unsigned envelope":    func(d *Draft) { d.SignedMIME = "From: attacker@example.test\r\n" + d.SignedMIME },
		"duplicate header":     func(d *Draft) { d.SignedMIME = "MIME-Version: 1.0\r\n" + d.SignedMIME },
		"bare LF":              func(d *Draft) { d.SignedMIME = strings.ReplaceAll(d.SignedMIME, "\r\n", "\n") },
		"bare CR":              func(d *Draft) { d.SignedMIME = strings.Replace(d.SignedMIME, "\r\n", "\r", 1) },
		"epilogue":             func(d *Draft) { d.SignedMIME += "Untrusted decoration\r\n" },
		"preamble": func(d *Draft) {
			d.SignedMIME = strings.Replace(d.SignedMIME, "\r\n\r\n--", "\r\n\r\nDecoration\r\n--", 1)
		},
		"HTML":                func(d *Draft) { d.SignedMIME = strings.Replace(d.SignedMIME, "text/plain", "text/html", 1) },
		"recursive multipart": func(d *Draft) { d.SignedMIME = strings.Replace(d.SignedMIME, "multipart/mixed", "multipart/signed", 1) },
		"wrong signature protocol": func(d *Draft) {
			d.SignedMIME = strings.ReplaceAll(d.SignedMIME, "application/pgp-signature", "text/plain")
		},
		"wrong micalg":         func(d *Draft) { d.SignedMIME = strings.Replace(d.SignedMIME, "pgp-sha512", "pgp-sha1", 1) },
		"nonbase64 attachment": func(d *Draft) { d.SignedMIME = strings.ReplaceAll(d.SignedMIME, "base64", "7bit") },
		"wrong armor": func(d *Draft) {
			d.SignedMIME = strings.Replace(d.SignedMIME, "BEGIN PGP SIGNATURE", "BEGIN PGP MESSAGE", 1)
		},
		"too large": func(d *Draft) { d.SignedMIME = strings.Repeat("a", (4<<20)+1) },
	} {
		t.Run(name, func(t *testing.T) {
			copy := d
			mutate(&copy)
			if _, _, err := Build("owner@example.test", copy, nil); err == nil {
				t.Fatal("unsafe signed entity accepted")
			}
		})
	}
}

func TestSignedOutboxOwnershipAndUnknownOutcomeNeverResend(t *testing.T) {
	s, _, db, sender, owner, bob, account := setup(t)
	ctx := context.Background()
	d := signedDraft(t)
	record, err := s.Queue(ctx, owner, account, nonce, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Queue(ctx, bob, account, nonce, d); err == nil {
		t.Fatal("foreign signed queue accepted")
	}
	if err = s.Dispatch(ctx, bob, record.ID); err == nil {
		t.Fatal("foreign signed dispatch accepted")
	}
	sender.outcome = mailnet.Unknown
	if err = s.Dispatch(ctx, owner, record.ID); err != nil {
		t.Fatal(err)
	}
	replay, err := s.Queue(ctx, owner, account, nonce, d)
	if err != nil || replay.ID != record.ID || replay.State != "unknown" {
		t.Fatal("uncertain nonce replay changed", err)
	}
	if err = s.Dispatch(ctx, owner, record.ID); err != nil {
		t.Fatal(err)
	}
	if sender.calls.Load() != 1 || sender.copies.Load() != 0 {
		t.Fatal("uncertain signed mail resent or copied")
	}
	var raw []byte
	if err = db.Database().QueryRow("SELECT raw FROM outbox WHERE id=?", record.ID).Scan(&raw); err != nil || !bytes.HasSuffix(raw, []byte(d.SignedMIME)) {
		t.Fatal("durable signed MIME changed", err)
	}
	changed := d
	changed.Subject = "changed"
	if _, err = s.Queue(ctx, owner, account, nonce, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("signed nonce accepted another input", err)
	}
}

func TestAddingSignedFieldsPreservesLegacyNonceHashInput(t *testing.T) {
	raw, err := json.Marshal(Draft{})
	const legacy = `{"SourceRevision":0,"ReplyOutgoing":"","EncryptedMIME":"","To":"","CC":"","BCC":"","Subject":"","Body":"","AllowPlaintext":false,"ReplyFolder":"","ReplyValidity":0,"ReplyUID":0}`
	if err != nil || string(raw) != legacy {
		t.Fatal("legacy nonce hash serialization changed", string(raw), err)
	}
}

func FuzzSignedTransportBoundary(f *testing.F) {
	fixture, err := os.ReadFile("testdata/signed.eml")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(string(fixture))
	// Overlapping opening/closing delimiters with no part must fail closed,
	// rather than producing a negative slice bound in the exact-octet parser.
	f.Add("MIME-Version: 1.0\r\nContent-Type: multipart/signed; protocol=\"application/pgp-signature\"; micalg=pgp-sha512; boundary=b\r\n\r\n--b\r\n--b--\r\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, entity string) {
		_, _, _ = Build("owner@example.test", Draft{To: "recipient@example.test", SignedMIME: entity, Provenance: "authored", AllowPlaintext: true}, nil)
	})
}
