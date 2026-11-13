// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"bytes"
	"mailspace.local/mailspace/internal/mailsync"
	"net/mail"
	"strings"
	"testing"
)

func TestBccEnvelopeUnicodeAndCRLF(t *testing.T) {
	d := draft()
	d.Subject = "Zażółć gęślą"
	d.Body = "Pierwsza linia\nDruga = linia ąę"
	d.CC = "Other <other@example.test>"
	parent := &mailsync.Preview{MessageID: "root@example.test", References: []string{"before@example.test"}}
	raw, to, err := Build("sender@example.test", d, parent)
	if err != nil {
		t.Fatal(err)
	}
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if m.Header.Get("Bcc") != "" || bytes.Contains(raw, []byte("hidden@example.test")) || len(to) != 3 {
		t.Fatal("Bcc leaked to MIME or missing envelope")
	}
	if m.Header.Get("In-Reply-To") != "<root@example.test>" || !strings.Contains(m.Header.Get("References"), "<before@example.test>") {
		t.Fatal("reply headers missing")
	}
	p := mailsync.Parse(raw)
	if p.Subject != d.Subject || strings.ReplaceAll(p.Text, "\r\n", "\n") != d.Body {
		t.Fatal("Unicode/canonical MIME roundtrip failed", p)
	}
	if bytes.Contains(bytes.ReplaceAll(raw, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatal("bare LF in MIME")
	}
}
func TestComposeConsentInjectionAndEncryptedDowngrade(t *testing.T) {
	d := draft()
	d.AllowPlaintext = false
	if _, _, err := Build("sender@example.test", d, nil); err != ErrEncryption {
		t.Fatal("silent unencrypted send")
	}
	d = draft()
	if _, _, err := Build("sender@example.test", d, &mailsync.Preview{Encrypted: true}); err != ErrEncryption {
		t.Fatal("encrypted reply downgraded")
	}
	for _, field := range []string{"to", "subject", "bcc"} {
		d = draft()
		switch field {
		case "to":
			d.To = "recipient@example.test\r\nBcc: hidden@example.test"
		case "subject":
			d.Subject = "Hi\nBcc: hidden@example.test"
		case "bcc":
			d.BCC = "x@example.test\r\nSubject: injection"
		}
		if _, _, err := Build("sender@example.test", d, nil); err == nil {
			t.Fatal("header injection accepted", field)
		}
	}
	d = draft()
	d.Body = strings.Repeat("x", (256<<10)+1)
	if _, _, err := Build("sender@example.test", d, nil); err == nil {
		t.Fatal("body bound ignored")
	}
}
