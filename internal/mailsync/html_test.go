// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"strings"
	"testing"
)

func TestHTMLOnlyBecomesInertTextAndPlainAlternativeWins(t *testing.T) {
	html := `<html><head><title>Not body</title><style>secret css</style></head><body><h1>Hello &amp; welcome</h1><p>Zażółć <b>gęślą</b>.</p><script>secret script</script><img src="https://tracker.example.test/pixel"><iframe>secret frame</iframe><table><tr><td>First</td><td>Second</td></tr></table></body></html>`
	raw := []byte("From: test@example.test\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + html)
	p := Parse(raw)
	if p.Status != "ready" || !strings.Contains(p.Text, "Hello & welcome") || !strings.Contains(p.Text, "Zażółć gęślą.") || strings.Contains(p.Text, "secret") || strings.Contains(p.Text, "tracker") || strings.Contains(p.Text, "<h1>") {
		t.Fatal("unsafe HTML conversion", p)
	}
	alt := []byte("Content-Type: multipart/alternative; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nNative text wins\r\n--b\r\nContent-Type: text/html\r\n\r\n<p>Do not replace plain</p>\r\n--b--\r\n")
	if p := Parse(alt); p.Text != "Native text wins" {
		t.Fatal("HTML overrode native text", p.Text)
	}
	opaque := []byte("Content-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/html\r\n\r\n<p>Private sibling canary</p>\r\n--b\r\nContent-Type: application/pgp-encrypted\r\n\r\nVersion: 1\r\n--b--\r\n")
	if p := Parse(opaque); !p.Encrypted || p.Text != "" {
		t.Fatal("HTML sibling escaped encrypted boundary", p)
	}
	if _, err := HTMLText([]byte(strings.Repeat("x", maxText+1))); err == nil {
		t.Fatal("unbounded HTML")
	}
}
