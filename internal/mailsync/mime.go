// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/emersion/go-message"
	"io"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxText = 64 << 10

var ErrMIME = errors.New("message has unsupported or excessive MIME")
var messageID = regexp.MustCompile(`<([^<>\s]{1,998})>`)

type Attachment struct {
	Name string
	Type string
}

type Preview struct {
	Attachments []Attachment
	ContentHash string
	Subject     string
	Sender      string
	ReplyTo     string
	To          []string
	Name        string
	MessageID   string
	References  []string
	Text        string
	Encrypted   bool
	Status      string
}

func ids(value string) []string {
	result := []string{}
	for _, v := range messageID.FindAllStringSubmatch(value, 64) {
		result = append(result, v[1])
	}
	return result
}
func text(value string, limit int) string {
	value = strings.ToValidUTF8(value, "�")
	value = strings.ReplaceAll(value, "\x00", "")
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

// Parse never decrypts or renders HTML. Any encrypted MIME part makes the whole
// preview opaque, including preceding plaintext siblings. Original MIME is kept.
func Parse(raw []byte) Preview {
	digest := sha256.Sum256(raw)
	contentHash := hex.EncodeToString(digest[:])
	result := Preview{ContentHash: contentHash, Subject: "(No subject)", Name: "Unknown sender", Status: "ready"}
	root, err := message.ReadWithOptions(bytes.NewReader(raw), &message.ReadOptions{MaxHeaderBytes: 64 << 10})
	if err != nil {
		result.Status = "unsupported"
		return result
	}
	if value, err := root.Header.Text("Subject"); err == nil && value != "" {
		result.Subject = text(value, 1000)
	}
	if from, err := mail.ParseAddress(root.Header.Get("From")); err == nil {
		result.Sender = text(from.Address, 254)
		result.Name = text(from.Name, 200)
		if result.Name == "" {
			result.Name = result.Sender
		}
	}
	if to, err := mail.ParseAddressList(root.Header.Get("To")); err == nil {
		for i, a := range to {
			if i >= 100 {
				break
			}
			result.To = append(result.To, text(a.Address, 254))
		}
	}
	if reply, err := mail.ParseAddress(root.Header.Get("Reply-To")); err == nil {
		result.ReplyTo = text(reply.Address, 254)
	}
	if values := ids(root.Header.Get("Message-ID")); len(values) == 1 {
		result.MessageID = values[0]
	}
	result.References = ids(root.Header.Get("References"))
	if len(result.References) == 0 {
		result.References = ids(root.Header.Get("In-Reply-To"))
	}
	parts := 0
	decoded := 0
	var plain []string
	var convertedHTML []string
	htmlDecoded := 0
	htmlTooLarge := false
	var visit func(*message.Entity, int) error
	visit = func(entity *message.Entity, depth int) error {
		parts++
		if depth > 12 || parts > 100 {
			return ErrMIME
		}
		media, params, err := entity.Header.ContentType()
		if err != nil {
			return ErrMIME
		}
		if media == "multipart/encrypted" || media == "application/pgp-encrypted" || media == "application/pkcs7-mime" || media == "application/x-pkcs7-mime" {
			result.Encrypted = true
			return nil
		}
		if strings.HasPrefix(media, "multipart/") {
			reader := entity.MultipartReader()
			if reader == nil {
				return ErrMIME
			}
			defer reader.Close()
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					return ErrMIME
				}
				if err = visit(part, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		disposition, dispositionParams, _ := entity.Header.ContentDisposition()
		if disposition == "attachment" || (media != "text/plain" && media != "text/html") {
			if media != "application/pgp-signature" {
				name := dispositionParams["filename"]
				if name == "" {
					name = params["name"]
				}
				result.Attachments = append(result.Attachments, Attachment{Name: text(name, 180), Type: text(media, 100)})
			}
			return nil
		}
		if media == "text/html" {
			value, err := io.ReadAll(io.LimitReader(entity.Body, int64(maxText-htmlDecoded+1)))
			if err != nil {
				return ErrMIME
			}
			htmlDecoded += len(value)
			if htmlDecoded > maxText {
				htmlTooLarge = true
				return nil
			}
			if !utf8.Valid(value) {
				return ErrMIME
			}
			if bytes.Contains(value, []byte("-----BEGIN PGP MESSAGE-----")) {
				result.Encrypted = true
				return nil
			}
			extracted, err := HTMLText(value)
			if err != nil {
				htmlTooLarge = true
				return nil
			}
			if strings.Contains(extracted, "-----BEGIN PGP MESSAGE-----") {
				result.Encrypted = true
				return nil
			}
			convertedHTML = append(convertedHTML, extracted)
			return nil
		}
		// Do not show mislabeled ciphertext as ordinary mail text.
		value, err := io.ReadAll(io.LimitReader(entity.Body, int64(maxText-decoded+1)))
		if err != nil {
			return ErrMIME
		}
		decoded += len(value)
		if decoded > maxText {
			return ErrMIME
		}
		if bytes.Contains(value, []byte("-----BEGIN PGP MESSAGE-----")) {
			result.Encrypted = true
			return nil
		}
		charset := strings.ToLower(params["charset"])
		if charset != "" && charset != "utf-8" && charset != "us-ascii" {
			return ErrMIME
		}
		if !utf8.Valid(value) {
			return ErrMIME
		}
		plain = append(plain, string(value))
		return nil
	}
	if err = visit(root, 0); err != nil {
		result.Status = "unsupported"
	}
	if result.Encrypted {
		result.Attachments = nil
		result.Text = ""
		result.Status = "encrypted"
		return result
	}
	if len(plain) == 0 {
		if htmlTooLarge {
			result.Status = "unsupported"
		} else if len(convertedHTML) > 0 {
			plain = convertedHTML
		}
	}
	if result.Status == "ready" {
		result.Text = text(strings.TrimSpace(strings.Join(plain, "\n\n")), maxText)
		if result.Text == "" {
			result.Text = "No readable text body. Attachments are available separately."
		}
	} else {
		result.Text = "No preview: unsupported or excessive MIME. Original message is preserved."
	}
	return result
}
