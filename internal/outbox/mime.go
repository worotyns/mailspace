// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"mailspace.local/mailspace/internal/mailsync"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInput = errors.New("invalid outgoing message")
var ErrEncryption = errors.New("explicit unencrypted send consent is required; encrypted replies require the client crypto module")

type Draft struct {
	SourceRevision int64
	ReplyOutgoing  string
	EncryptedMIME  string
	To             string
	CC             string
	BCC            string
	Subject        string
	Body           string
	AllowPlaintext bool
	ReplyFolder    string
	ReplyValidity  uint32
	ReplyUID       uint32
}

func (Draft) String() string   { return "<redacted outgoing draft>" }
func (Draft) GoString() string { return "<redacted outgoing draft>" }
func addresses(value string) ([]string, []string, error) {
	if value == "" {
		return nil, nil, nil
	}
	if len(value) > 8192 || strings.ContainsAny(value, "\x00\r\n") {
		return nil, nil, ErrInput
	}
	parsed, err := mail.ParseAddressList(value)
	if err != nil || len(parsed) > 100 {
		return nil, nil, ErrInput
	}
	headers := []string{}
	envelope := []string{}
	for _, a := range parsed {
		if len(a.Address) > 254 || len(a.Name) > 200 || !strings.Contains(a.Address, "@") || strings.ContainsAny(a.Address, "\r\n\x00") {
			return nil, nil, ErrInput
		}
		for _, r := range a.Address {
			if r > 127 || r < 33 {
				return nil, nil, ErrInput
			}
		}
		headers = append(headers, a.String())
		envelope = append(envelope, a.Address)
	}
	return headers, envelope, nil
}

// Build constructs CRLF MIME with no Bcc header. Hidden recipients belong only
// to the SMTP envelope and the owner's private outbox metadata.
func Build(from string, d Draft, parent *mailsync.Preview) ([]byte, []string, error) {
	if d.EncryptedMIME != "" {
		return buildEncrypted(from, d, parent)
	}
	if !d.AllowPlaintext {
		return nil, nil, ErrEncryption
	}
	if !utf8.ValidString(d.Body) || len(d.Body) > 256<<10 || strings.ContainsRune(d.Body, '\x00') || !utf8.ValidString(d.Subject) || len(d.Subject) > 200 || strings.ContainsAny(d.Subject, "\r\n\x00") {
		return nil, nil, ErrInput
	}
	fromHeader, _, err := addresses(from)
	if err != nil || len(fromHeader) != 1 {
		return nil, nil, ErrInput
	}
	tos, to, err := addresses(d.To)
	if err != nil {
		return nil, nil, err
	}
	ccs, cc, err := addresses(d.CC)
	if err != nil {
		return nil, nil, err
	}
	_, bcc, err := addresses(d.BCC)
	if err != nil {
		return nil, nil, err
	}
	envelope := []string{}
	seen := map[string]bool{}
	for _, v := range append(append(to, cc...), bcc...) {
		if !seen[v] {
			seen[v] = true
			envelope = append(envelope, v)
		}
	}
	if len(envelope) == 0 || len(envelope) > 100 {
		return nil, nil, ErrInput
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "From: %s\r\n", fromHeader[0])
	if len(tos) > 0 {
		fmt.Fprintf(&out, "To: %s\r\n", strings.Join(tos, ",\r\n "))
	} else {
		fmt.Fprint(&out, "To: undisclosed-recipients:;\r\n")
	}
	if len(ccs) > 0 {
		fmt.Fprintf(&out, "Cc: %s\r\n", strings.Join(ccs, ",\r\n "))
	}
	fmt.Fprintf(&out, "Subject: %s\r\nDate: %s\r\nMessage-ID: <%s@mailspace.invalid>\r\n", mime.QEncoding.Encode("utf-8", d.Subject), time.Now().UTC().Format(time.RFC1123Z), rand.Text())
	if parent != nil {
		if parent.Encrypted {
			return nil, nil, ErrEncryption
		}
		if parent.MessageID != "" {
			refs := append(append([]string{}, parent.References...), parent.MessageID)
			if len(refs) > 32 {
				refs = refs[len(refs)-32:]
			}
			fmt.Fprintf(&out, "In-Reply-To: <%s>\r\nReferences:", parent.MessageID)
			for _, ref := range refs {
				if strings.ContainsAny(ref, "\r\n<> \x00") || len(ref) > 900 {
					return nil, nil, ErrInput
				}
				fmt.Fprintf(&out, "\r\n <%s>", ref)
			}
			out.WriteString("\r\n")
		}
	}
	out.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	writer := quotedprintable.NewWriter(&out)
	body := strings.ReplaceAll(strings.ReplaceAll(d.Body, "\r\n", "\n"), "\r", "\n")
	if _, err = writer.Write([]byte(body)); err != nil {
		return nil, nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, nil, err
	}
	if !bytes.HasSuffix(out.Bytes(), []byte("\r\n")) {
		out.WriteString("\r\n")
	}
	return out.Bytes(), envelope, nil
}
