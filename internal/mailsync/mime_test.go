// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"strings"
	"testing"
	"time"
)

func TestMIMEPreviewOpaqueEncryptedAndHTML(t *testing.T) {
	p := Parse(synthetic(1, ""))
	if p.Text != "Synthetic body 1" || p.MessageID != "m1@example.test" {
		t.Fatal(p)
	}
	html := Parse([]byte("From: x@example.test\r\nContent-Type: text/html\r\n\r\n<script>alert(1)</script><img src=https://tracking.example.test>"))
	if strings.Contains(html.Text, "<script>") || strings.Contains(html.Text, "tracking") {
		t.Fatal("HTML leaked into preview")
	}
	for _, media := range []string{"multipart/encrypted; boundary=e; protocol=\"application/pgp-encrypted\"", "application/pgp-encrypted", "application/pkcs7-mime"} {
		p = Parse([]byte("Content-Type: " + media + "\r\n\r\nsecret"))
		if !p.Encrypted || p.Text != "" {
			t.Fatal("encrypted MIME exposed", media, p)
		}
	}
	p = Parse([]byte("Content-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nMUST-NOT-DISPLAY\r\n--x\r\nContent-Type: application/pgp-encrypted\r\n\r\nVersion: 1\r\n--x--\r\n"))
	if !p.Encrypted || p.Text != "" {
		t.Fatal("mixed encrypted message exposed sibling text", p)
	}
	p = Parse([]byte("Content-Type: text/plain\r\n\r\n-----BEGIN PGP MESSAGE-----\r\nopaque"))
	if !p.Encrypted || p.Text != "" {
		t.Fatal("inline ciphertext displayed")
	}
	p = Parse([]byte("Content-Type: text/plain\r\n\r\n" + strings.Repeat("a", maxText+1)))
	if p.Status != "unsupported" || strings.Contains(p.Text, strings.Repeat("a", 100)) {
		t.Fatal("decoded budget ignored")
	}
	nested := "Content-Type: text/plain\r\n\r\nleaf"
	for i := 0; i < 15; i++ {
		nested = "Content-Type: multipart/mixed; boundary=b" + strings.Repeat("x", i) + "\r\n\r\n--b" + strings.Repeat("x", i) + "\r\n" + nested + "\r\n--b" + strings.Repeat("x", i) + "--\r\n"
	}
	if p = Parse([]byte(nested)); p.Status != "unsupported" {
		t.Fatal("nesting budget ignored")
	}
}
func TestThreadingDuplicatesMissingReferencesAndCycles(t *testing.T) {
	makeMessage := func(uid uint32, id string, refs ...string) CachedMessage {
		return CachedMessage{Mailbox: "INBOX", UID: uid, Validity: 1, Received: time.Unix(int64(uid), 0), Preview: Preview{Name: "x", Subject: "same subject", MessageID: id, References: refs}}
	}
	m := []CachedMessage{makeMessage(1, "duplicate"), makeMessage(2, "duplicate"), makeMessage(3, "child", "duplicate"), makeMessage(4, "missing", "absent")}
	if len(Conversations(m)) != 4 {
		t.Fatal("duplicate/missing id or identical subject merged unrelated messages")
	}
	m = []CachedMessage{makeMessage(1, "a", "b"), makeMessage(2, "b", "a")}
	if c := Conversations(m); len(c) != 1 || len(c[0].Messages) != 2 {
		t.Fatal("cycle not bounded")
	}
}
