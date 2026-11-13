// SPDX-License-Identifier: AGPL-3.0-only
package mail

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

type Locator struct {
	AccountID   string
	Mailbox     string
	UIDValidity uint32
	UID         uint32
}
type Attachment struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Size string `json:"size"`
}
type Message struct {
	Timestamp   string       `json:"timestamp,omitempty"`
	Locator     *Locator     `json:"locator,omitempty"`
	ID          string       `json:"id"`
	Sender      string       `json:"sender"`
	Name        string       `json:"name"`
	Initials    string       `json:"initials"`
	Tone        string       `json:"tone"`
	Time        string       `json:"time"`
	Text        string       `json:"text,omitempty"`
	Encrypted   bool         `json:"encrypted"`
	Attachments []Attachment `json:"attachments"`
}
type Conversation struct {
	ID       string    `json:"id"`
	Subject  string    `json:"subject"`
	Channel  string    `json:"channel"`
	Folder   string    `json:"folder"`
	Tags     []string  `json:"tags"`
	Messages []Message `json:"messages"`
	Unread   bool      `json:"unread"`
	Starred  bool      `json:"starred"`
}

func (m Message) PreviewText() string {
	runes := []rune(m.Text)
	if len(runes) > 400 {
		return string(runes[:400]) + "…"
	}
	return m.Text
}

func (c Conversation) Last() Message      { return c.Messages[len(c.Messages)-1] }
func (c Conversation) Root() Message      { return c.Messages[0] }
func (c Conversation) Replies() []Message { return c.Messages[1:] }
func (c Conversation) ReplyCount() int    { return len(c.Messages) - 1 }
func (c Conversation) LastReply() string  { return c.Messages[len(c.Messages)-1].Time }

// Transport preserves original MIME bytes. There is deliberately no server decrypt port.
type Transport interface {
	FetchMIME(context.Context, Locator) ([]byte, error)
	SendMIME(context.Context, []string, []byte) error
}

//go:embed testdata/demo.json
var demoJSON []byte

// Demo returns a fresh copy; requests cannot mutate shared fixture data.
func Demo() []Conversation {
	var result []Conversation
	if err := json.Unmarshal(demoJSON, &result); err != nil {
		panic("invalid bundled synthetic seed")
	}
	return result
}

// ValidateDemo refuses invalid fixtures and non-synthetic addresses.
func ValidateDemo() error {
	ids := map[string]bool{}
	for _, c := range Demo() {
		if c.ID == "" || ids[c.ID] || len(c.Messages) == 0 {
			return fmt.Errorf("invalid seed conversation")
		}
		ids[c.ID] = true
		if c.Channel != "general" && c.Channel != "design" && c.Channel != "engineering" && c.Channel != "product" {
			return fmt.Errorf("invalid seed channel")
		}
		for _, m := range c.Messages {
			if m.ID == "" || ids[m.ID] || !strings.HasSuffix(m.Sender, ".test") || m.Name == "" {
				return fmt.Errorf("invalid seed message")
			}
			ids[m.ID] = true
			if m.Encrypted && m.Text != "" {
				return fmt.Errorf("encrypted fixture must not contain plaintext")
			}
		}
	}
	return nil
}

// SourceRef identifies an original authorized mail resource, never a Space or a
// synthesized SMTP thread. Outbox is used before/without an IMAP Sent copy.
type SourceRef struct {
	Revision   int64  `json:"accountRevision"`
	Kind       string `json:"kind"`
	AccountID  string `json:"accountId"`
	Mailbox    string `json:"mailbox,omitempty"`
	Validity   uint32 `json:"validity,omitempty"`
	UID        uint32 `json:"uid,omitempty"`
	OutgoingID string `json:"outgoingId,omitempty"`
}
type MessageIdentity struct {
	Key       string
	AccountID string
	Revision  int64
}
