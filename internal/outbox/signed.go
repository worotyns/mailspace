// SPDX-License-Identifier: AGPL-3.0-only
package outbox

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"net/mail"
	"strings"
	"unicode/utf8"

	"mailspace.local/mailspace/internal/mailsync"
)

// buildSigned validates an authored transport format, not signature validity or
// signer trust. Authenticated inner bytes must never be normalized or rebuilt.
func buildSigned(from string, d Draft, parent *mailsync.Preview) ([]byte, []string, error) {
	if !d.AllowPlaintext {
		return nil, nil, ErrEncryption
	}
	if d.Provenance != "authored" || d.Body != "" || d.EncryptedMIME != "" {
		return nil, nil, ErrInput
	}
	entity := []byte(d.SignedMIME)
	if len(entity) > 4<<20 || !canonicalSigned(entity) {
		return nil, nil, ErrInput
	}
	header, body, err := signedHeaders(entity)
	if err != nil || !onlyHeaders(header, "Mime-Version", "Content-Type") || header.Get("MIME-Version") != "1.0" {
		return nil, nil, ErrInput
	}
	kind, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || kind != "multipart/signed" || len(params) != 3 || params["protocol"] != "application/pgp-signature" || params["micalg"] != "pgp-sha512" {
		return nil, nil, ErrInput
	}
	parts, err := exactSignedParts(body, params["boundary"])
	if err != nil || len(parts) != 2 || !validAuthoredEntity(parts[0]) {
		return nil, nil, ErrInput
	}
	sigHeader, armor, err := signedHeaders(parts[1])
	if err != nil || !onlyHeaders(sigHeader, "Content-Type", "Content-Disposition") || sigHeader.Get("Content-Type") != "application/pgp-signature" || sigHeader.Get("Content-Disposition") != `attachment; filename="signature.asc"` || !validArmor(armor, "SIGNATURE") {
		return nil, nil, ErrInput
	}
	metadata := d
	metadata.SignedMIME, metadata.Provenance = "", ""
	// Build enforces encrypted-parent refusal and supplies authoritative envelope
	// headers, reply references and SMTP recipients. The signature covers neither.
	raw, recipients, err := Build(from, metadata, parent)
	if err != nil {
		return nil, nil, err
	}
	marker := bytes.Index(raw, []byte("MIME-Version: 1.0\r\n"))
	if marker < 0 {
		return nil, nil, ErrInput
	}
	return append(raw[:marker:marker], entity...), recipients, nil
}

func canonicalSigned(raw []byte) bool {
	if !bytes.HasSuffix(raw, []byte("\r\n")) {
		return false
	}
	for _, line := range bytes.Split(raw, []byte("\r\n")) {
		if len(line) > 998 || bytes.HasSuffix(line, []byte(" ")) || bytes.HasSuffix(line, []byte("\t")) {
			return false
		}
		for _, value := range line {
			if value < 32 || value > 126 {
				return false
			}
		}
	}
	return true
}

func signedHeaders(entity []byte) (mail.Header, []byte, error) {
	end := bytes.Index(entity, []byte("\r\n\r\n"))
	if end < 0 || end > 64<<10 {
		return nil, nil, ErrInput
	}
	message, err := mail.ReadMessage(bufio.NewReader(bytes.NewReader(entity)))
	if err != nil {
		return nil, nil, ErrInput
	}
	return message.Header, entity[end+4:], nil
}

func onlyHeaders(header mail.Header, allowed ...string) bool {
	if len(header) != len(allowed) {
		return false
	}
	for _, name := range allowed {
		if len(header[name]) != 1 {
			return false
		}
	}
	return true
}

// Require exact delimiters and no preamble/epilogue, avoiding parsers which
// discard decorations or rewrite the first part before forwarding signed mail.
func exactSignedParts(body []byte, boundary string) ([][]byte, error) {
	if len(boundary) < 1 || len(boundary) > 70 || strings.ContainsAny(boundary, "\r\n") {
		return nil, ErrInput
	}
	prefix, suffix := []byte("--"+boundary+"\r\n"), []byte("\r\n--"+boundary+"--\r\n")
	if len(body) < len(prefix)+len(suffix) || !bytes.HasPrefix(body, prefix) || !bytes.HasSuffix(body, suffix) {
		return nil, ErrInput
	}
	body = body[len(prefix) : len(body)-len(suffix)]
	parts := bytes.Split(body, []byte("\r\n--"+boundary+"\r\n"))
	for _, part := range parts {
		if bytes.Contains(part, []byte("\r\n--"+boundary)) {
			return nil, ErrInput
		}
	}
	return parts, nil
}

func validAuthoredEntity(entity []byte) bool {
	header, body, err := signedHeaders(entity)
	if err != nil {
		return false
	}
	kind, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		return false
	}
	if kind == "text/plain" {
		return validAuthoredText(header, body)
	}
	if kind != "multipart/mixed" || len(params) != 1 || !onlyHeaders(header, "Content-Type") {
		return false
	}
	parts, err := exactSignedParts(body, params["boundary"])
	if err != nil || len(parts) < 2 || len(parts) > 11 {
		return false
	}
	textHeader, text, err := signedHeaders(parts[0])
	if err != nil || !validAuthoredText(textHeader, text) {
		return false
	}
	total := 0
	for _, part := range parts[1:] {
		h, data, err := signedHeaders(part)
		if err != nil || !onlyHeaders(h, "Content-Type", "Content-Transfer-Encoding", "Content-Disposition") || h.Get("Content-Type") != "application/octet-stream" || h.Get("Content-Transfer-Encoding") != "base64" {
			return false
		}
		disposition, parameters, err := mime.ParseMediaType(h.Get("Content-Disposition"))
		name := parameters["filename"]
		if err != nil || disposition != "attachment" || len(parameters) != 1 || name == "" || len(name) > 180 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\") || strings.IndexFunc(name, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
			return false
		}
		decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding.Strict(), bytes.NewReader(data)))
		total += len(decoded)
		if err != nil || total > 1<<20 {
			return false
		}
	}
	return true
}

func validAuthoredText(header mail.Header, body []byte) bool {
	kind, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || kind != "text/plain" || len(params) != 1 || params["charset"] != "utf-8" || !onlyHeaders(header, "Content-Type", "Content-Transfer-Encoding") || header.Get("Content-Transfer-Encoding") != "base64" {
		return false
	}
	decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding.Strict(), bytes.NewReader(body)))
	return err == nil && len(decoded) <= 256<<10 && utf8.Valid(decoded) && !bytes.ContainsRune(decoded, 0)
}
