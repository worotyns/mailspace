// SPDX-License-Identifier: AGPL-3.0-only
package mailnet

import (
	"context"
	"errors"
	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
	"io"
	"net"
	"strings"
	"time"
)

const MaxCachedMessages = 500
const MaxMailboxes = 32
const MaxMIMEBytes = 4 << 20

var ErrMailLimit = errors.New("mail synchronization limit reached")

type Mailbox struct {
	Name        string
	Total       uint32
	UIDValidity uint32
	Messages    []RemoteMessage
}
type RemoteMessage struct {
	UID      uint32
	Flags    []string
	Size     int64
	Received time.Time
}

// ReadSession is an internal, authenticated transport boundary. It offers only
// read-only selection and BODY.PEEK; it cannot mutate flags or send mail.
type ReadSession interface {
	Mailboxes() ([]string, error)
	Snapshot(string) (Mailbox, error)
	MIME(string, uint32, uint32) ([]byte, error)
	Close() error
}
type Reader interface {
	OpenRead(context.Context, Endpoint, Login) (ReadSession, error)
}
type readSession struct {
	client   *imapclient.Client
	cancel   context.CancelFunc
	selected string
	validity uint32
	conn     net.Conn
	pending  io.Closer
}

func (c *Connector) OpenRead(ctx context.Context, ep Endpoint, login Login) (ReadSession, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	conn, config, err := c.connection(ctx, ep)
	if err != nil {
		cancel()
		return nil, CheckError{"IMAP", "endpoint/TLS"}
	}
	options := &imapclient.Options{TLSConfig: config}
	var client *imapclient.Client
	if ep.TLS == "starttls" {
		client, err = imapclient.NewStartTLS(conn, options)
	} else {
		client = imapclient.New(conn, options)
	}
	if err != nil {
		conn.Close()
		cancel()
		return nil, CheckError{"IMAP", "STARTTLS"}
	}
	if client.Caps().Has(imap.AuthCap("PLAIN")) {
		err = client.Authenticate(sasl.NewPlainClient("", login.Username, login.Password))
	} else {
		err = client.Login(login.Username, login.Password).Wait()
	}
	if err != nil {
		client.Close()
		cancel()
		return nil, CheckError{"IMAP", "authentication"}
	}
	return &readSession{client: client, cancel: cancel, conn: conn}, nil
}
func (s *readSession) Close() error {
	s.cancel()
	_ = s.conn.Close()
	// The upstream streaming decoder waits for literals to be consumed. Drain its
	// command AFTER closing the socket, BEFORE waiting for the decoder in Close.
	// Reversing that order deadlocks on a rejected oversized literal.
	if s.pending != nil {
		pending := s.pending
		s.pending = nil
		_ = pending.Close()
	}
	return s.client.Close()
}
func (s *readSession) fail() error { s.Close(); return CheckError{"IMAP", "read"} }
func (s *readSession) Mailboxes() ([]string, error) {
	cmd := s.client.List("", "*", nil)
	s.pending = cmd
	result := []string{}
	seen := map[string]bool{}
	for item := cmd.Next(); item != nil; item = cmd.Next() {
		selectable := true
		for _, a := range item.Attrs {
			if a == imap.MailboxAttrNoSelect {
				selectable = false
			}
		}
		if !selectable {
			continue
		}
		if len(result) >= MaxMailboxes || len(item.Mailbox) > 1024 || strings.ContainsAny(item.Mailbox, "\x00\r\n") {
			s.Close()
			return nil, ErrMailLimit
		}
		if strings.EqualFold(item.Mailbox, "INBOX") {
			item.Mailbox = "INBOX"
		}
		if seen[item.Mailbox] {
			s.Close()
			return nil, s.fail()
		}
		seen[item.Mailbox] = true
		result = append(result, item.Mailbox)
	}
	if err := cmd.Close(); err != nil {
		return nil, s.fail()
	}
	s.pending = nil
	return result, nil
}
func (s *readSession) selectMailbox(name string) (*imap.SelectData, error) {
	data, err := s.client.Select(name, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil || data.UIDValidity == 0 {
		return nil, s.fail()
	}
	s.selected = name
	s.validity = data.UIDValidity
	return data, nil
}
func (s *readSession) Snapshot(name string) (Mailbox, error) {
	selected, err := s.selectMailbox(name)
	if err != nil {
		return Mailbox{}, err
	}
	box := Mailbox{Name: name, Total: selected.NumMessages, UIDValidity: selected.UIDValidity}
	if selected.NumMessages == 0 {
		return box, nil
	}
	first := uint32(1)
	if selected.NumMessages > MaxCachedMessages {
		first = selected.NumMessages - MaxCachedMessages + 1
	}
	seen := map[uint32]bool{}
	for start := first; start <= selected.NumMessages; {
		end := start + 49
		if end < start || end > selected.NumMessages {
			end = selected.NumMessages
		}
		cmd := s.client.Fetch(imap.SeqSet{{Start: start, Stop: end}}, &imap.FetchOptions{UID: true, Flags: true, InternalDate: true, RFC822Size: true})
		s.pending = cmd
		for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
			value := RemoteMessage{}
			for item := msg.Next(); item != nil; item = msg.Next() {
				switch v := item.(type) {
				case imapclient.FetchItemDataUID:
					value.UID = uint32(v.UID)
				case imapclient.FetchItemDataFlags:
					for _, flag := range v.Flags {
						if len(value.Flags) >= 128 || len(flag) > 256 {
							s.Close()
							return Mailbox{}, ErrMailLimit
						}
						value.Flags = append(value.Flags, string(flag))
					}
				case imapclient.FetchItemDataRFC822Size:
					value.Size = v.Size
				case imapclient.FetchItemDataInternalDate:
					value.Received = v.Time
				case imapclient.FetchItemDataBodySection:
					s.Close()
					return Mailbox{}, ErrMailLimit
				}
			}
			if value.UID == 0 || seen[value.UID] || len(box.Messages) >= MaxCachedMessages || value.Size < 0 {
				s.Close()
				return Mailbox{}, s.fail()
			}
			seen[value.UID] = true
			box.Messages = append(box.Messages, value)
		}
		if err = cmd.Close(); err != nil {
			return Mailbox{}, s.fail()
		}
		s.pending = nil
		if end == selected.NumMessages {
			break
		}
		start = end + 1
	}
	// If EXPUNGE shifted sequence numbers during paging, retry the snapshot rather
	// than committing an ambiguous deletion set. UID identity is never sequence id.
	after, err := s.client.Select(name, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil || after.UIDValidity != box.UIDValidity || after.NumMessages != box.Total || len(box.Messages) != int(box.Total-first+1) {
		return Mailbox{}, s.fail()
	}
	return box, nil
}
func (s *readSession) MIME(name string, validity, uid uint32) ([]byte, error) {
	if s.selected != name {
		if _, err := s.selectMailbox(name); err != nil {
			return nil, err
		}
	}
	if s.validity != validity {
		return nil, s.fail()
	}
	section := &imap.FetchItemBodySection{Peek: true, Partial: &imap.SectionPartial{Size: MaxMIMEBytes + 1}}
	cmd := s.client.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{section}})
	s.pending = cmd
	var raw []byte
	var got uint32
	count := 0
	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		count++
		if count > 1 {
			s.Close()
			return nil, s.fail()
		}
		for item := msg.Next(); item != nil; item = msg.Next() {
			switch v := item.(type) {
			case imapclient.FetchItemDataUID:
				got = uint32(v.UID)
			case imapclient.FetchItemDataBodySection:
				if raw != nil || v.Literal == nil || v.Literal.Size() > MaxMIMEBytes {
					s.Close()
					return nil, ErrMailLimit
				}
				var err error
				raw, err = io.ReadAll(io.LimitReader(v.Literal, MaxMIMEBytes+1))
				if err != nil {
					return nil, s.fail()
				}
				if len(raw) > MaxMIMEBytes {
					s.Close()
					return nil, ErrMailLimit
				}
			}
		}
	}
	if err := cmd.Close(); err != nil || got != uid || raw == nil {
		return nil, s.fail()
	}
	s.pending = nil
	return raw, nil
}
