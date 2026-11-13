// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"

	"mailspace.local/mailspace/internal/mailsync"
)

// Encrypted MIME is an opaque transport boundary, not server-side cryptographic
// verification. Only the browser may decrypt or validate its inner signatures.
func buildEncrypted(from string, d Draft, parent *mailsync.Preview) ([]byte, []string, error) {
	if d.Body != "" || d.AllowPlaintext || len(d.EncryptedMIME) > 4<<20 {
		return nil, nil, ErrInput
	}
	entity := []byte(d.EncryptedMIME)
	if !bytes.HasSuffix(entity, []byte("\r\n")) || bytes.Contains(bytes.ReplaceAll(entity, []byte("\r\n"), nil), []byte("\n")) {
		return nil, nil, ErrInput
	}
	if headerEnd := bytes.Index(entity, []byte("\r\n\r\n")); headerEnd < 0 || headerEnd > 64<<10 {
		return nil, nil, ErrInput
	}
	message, err := mail.ReadMessage(bufio.NewReader(bytes.NewReader(entity)))
	if err != nil {
		return nil, nil, ErrInput
	}
	if len(message.Header["Content-Type"]) != 1 || len(message.Header["Mime-Version"]) != 1 || message.Header.Get("MIME-Version") != "1.0" {
		return nil, nil, ErrInput
	}
	for key := range message.Header {
		if key != "Content-Type" && key != "Mime-Version" {
			return nil, nil, ErrInput
		}
	}
	kind, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/encrypted" || params["protocol"] != "application/pgp-encrypted" || len(params["boundary"]) < 1 || len(params["boundary"]) > 70 {
		return nil, nil, ErrInput
	}
	reader := multipart.NewReader(message.Body, params["boundary"])
	control, err := reader.NextRawPart()
	if err != nil || control.Header.Get("Content-Type") != "application/pgp-encrypted" || control.Header.Get("Content-Transfer-Encoding") != "" {
		return nil, nil, ErrInput
	}
	version, err := io.ReadAll(io.LimitReader(control, 64))
	if err != nil || strings.TrimSpace(string(version)) != "Version: 1" {
		return nil, nil, ErrInput
	}
	part, err := reader.NextRawPart()
	if err != nil {
		return nil, nil, ErrInput
	}
	partType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
	if err != nil || partType != "application/octet-stream" || (part.Header.Get("Content-Transfer-Encoding") != "" && part.Header.Get("Content-Transfer-Encoding") != "7bit") {
		return nil, nil, ErrInput
	}
	armored, err := io.ReadAll(io.LimitReader(part, 4<<20))
	if err != nil || !validMessageArmor(armored) {
		return nil, nil, ErrInput
	}
	if _, err = reader.NextRawPart(); err != io.EOF {
		return nil, nil, ErrInput
	}
	// Generate authoritative From/Date/Message-ID/References server-side. The
	// encrypted payload and its embedded signature bytes are never rewritten.
	metadata := d
	metadata.EncryptedMIME = ""
	metadata.AllowPlaintext = true
	if parent != nil {
		copy := *parent
		copy.Encrypted = false
		parent = &copy
	}
	raw, to, err := Build(from, metadata, parent)
	if err != nil {
		return nil, nil, err
	}
	marker := bytes.Index(raw, []byte("MIME-Version: 1.0\r\n"))
	if marker < 0 {
		return nil, nil, ErrInput
	}
	var safe bytes.Buffer
	writer := multipart.NewWriter(&safe)
	fmt.Fprintf(&safe, "MIME-Version: 1.0\r\nContent-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=\"%s\"\r\n\r\n", writer.Boundary())
	first, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/pgp-encrypted"}})
	if err != nil {
		return nil, nil, err
	}
	_, _ = io.WriteString(first, "Version: 1\r\n")
	second, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/octet-stream"}, "Content-Disposition": {"inline; filename=\"encrypted.asc\""}})
	if err != nil {
		return nil, nil, err
	}
	_, _ = second.Write(armored)
	if err = writer.Close(); err != nil {
		return nil, nil, err
	}
	return append(raw[:marker], safe.Bytes()...), to, nil
}
func validMessageArmor(raw []byte) bool {
	text := strings.TrimSpace(strings.ReplaceAll(string(raw), "\r\n", "\n"))
	const begin = "-----BEGIN PGP MESSAGE-----\n\n"
	const end = "\n-----END PGP MESSAGE-----"
	if !strings.HasPrefix(text, begin) || !strings.HasSuffix(text, end) {
		return false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(text, begin), end)
	lines := strings.Split(body, "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[len(lines)-1], "=") {
		crc, e := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(lines[len(lines)-1], "="))
		if e != nil || len(crc) != 3 {
			return false
		}
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		if len(line) == 0 || len(line) > 76 {
			return false
		}
	}
	data, err := base64.StdEncoding.Strict().DecodeString(strings.Join(lines, ""))
	// Structural armor validation only. This is not a proof of recipient trust,
	// packet integrity or decryptability; the client owns those checks.
	return err == nil && len(data) > 32 && data[0]&0x80 != 0
}
