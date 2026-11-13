// SPDX-License-Identifier: AGPL-3.0-only
// Package timeline presents authorized individual messages independently of
// email-thread grouping. It has no provider/model client or content index.
package timeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrNotFound = errors.New("message source not found")
var ErrInput = errors.New("invalid message source")

type Record struct {
	Identity       mail.MessageIdentity
	Ref            mail.SourceRef
	AccountName    string
	AccountAddress string
	Preview        mailsync.Preview
	At             time.Time
	Unread         bool
	Own            bool
	State          string
	CopyState      string
}

func (Record) String() string   { return "<redacted timeline message>" }
func (Record) GoString() string { return "<redacted timeline message>" }

type Resolved struct {
	Record Record
	Raw    []byte
}

func (Resolved) String() string   { return "<redacted source message>" }
func (Resolved) GoString() string { return "<redacted source message>" }

type Service struct {
	users    *identity.Store
	accounts *accounts.Service
	sync     *mailsync.Service
	outbox   *outbox.Service
}

func New(users *identity.Store, a *accounts.Service, s *mailsync.Service, o *outbox.Service) *Service {
	return &Service{users: users, accounts: a, sync: s, outbox: o}
}
func identityFor(account string, revision int64, p mailsync.Preview, ref mail.SourceRef) mail.MessageIdentity {
	messageID := p.MessageID
	if messageID == "" {
		messageID = fmt.Sprintf("physical:%s:%s:%d:%d:%s", ref.Kind, ref.Mailbox, ref.Validity, ref.UID, ref.OutgoingID)
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s", account, revision, messageID, p.ContentHash)))
	return mail.MessageIdentity{AccountID: account, Revision: revision, Key: hex.EncodeToString(digest[:])}
}
func validRef(ref mail.SourceRef) bool {
	if ref.Revision < 1 || len(ref.AccountID) < 16 || len(ref.AccountID) > 128 {
		return false
	}
	switch ref.Kind {
	case "imap":
		return ref.UID > 0 && ref.Validity > 0 && len(ref.Mailbox) > 0 && len(ref.Mailbox) <= 1024 && utf8.ValidString(ref.Mailbox) && !strings.ContainsRune(ref.Mailbox, '\x00') && ref.OutgoingID == ""
	case "outbox":
		return len(ref.OutgoingID) >= 16 && len(ref.OutgoingID) <= 128 && ref.Mailbox == "" && ref.UID == 0 && ref.Validity == 0
	}
	return false
}
func (s *Service) Resolve(ctx context.Context, owner string, ref mail.SourceRef) (Resolved, error) {
	var result Resolved
	if !validRef(ref) {
		return result, ErrInput
	}
	account, err := s.accounts.Get(ctx, owner, ref.AccountID)
	if err != nil {
		return result, ErrNotFound
	}
	if account.Revision != ref.Revision {
		return result, ErrNotFound
	}
	var raw []byte
	var at time.Time
	state, copyState := "", ""
	var unread bool
	if ref.Kind == "imap" {
		var source mailsync.Source
		source, err = s.sync.Source(ctx, owner, ref.AccountID, ref.Mailbox, ref.Validity, ref.UID, ref.Revision)
		raw = source.Raw
		at = source.At
		unread = true
		for _, flag := range source.Flags {
			if flag == `\Seen` {
				unread = false
			}
		}
	} else {
		var source outbox.Source
		source, err = s.outbox.RawSource(ctx, owner, ref.AccountID, ref.OutgoingID)
		if err == nil && source.Revision != ref.Revision {
			return result, ErrNotFound
		}
		raw = source.Raw
		at = source.Record.CreatedAt
		state = source.Record.State
		copyState = source.Record.CopyState
	}
	if err != nil {
		return result, ErrNotFound
	}
	preview := mailsync.Parse(raw)
	result.Raw = raw
	result.Record = Record{Identity: identityFor(account.ID, account.Revision, preview, ref), Ref: ref, AccountName: account.Name, AccountAddress: account.Address, Preview: preview, At: at, Unread: unread, Own: strings.EqualFold(account.Address, preview.Sender), State: state, CopyState: copyState}
	return result, nil
}
func (s *Service) Identity(ctx context.Context, owner string, ref mail.SourceRef) (mail.MessageIdentity, error) {
	r, err := s.Resolve(ctx, owner, ref)
	return r.Record.Identity, err
}

// List is a bounded recent-message view, not a full mailbox-history claim.
// Identical Message-ID+raw-hash copies collapse only inside the same account and
// connection revision; conflicting IDs remain separate messages.
func (s *Service) List(ctx context.Context, owner, accountFilter string) ([]Record, error) {
	configured, err := s.users.Accounts(ctx, owner)
	if err != nil {
		return nil, err
	}
	found := accountFilter == ""
	records := map[string]Record{}
	for _, item := range configured {
		if accountFilter != "" && item.ID != accountFilter {
			continue
		}
		found = true
		account, err := s.accounts.Get(ctx, owner, item.ID)
		if err != nil {
			return nil, err
		}
		folders, err := s.sync.Folders(ctx, owner, item.ID)
		if err != nil {
			return nil, err
		}
		for _, folder := range folders {
			messages, err := s.sync.Messages(ctx, owner, item.ID, folder.Name)
			if err != nil {
				return nil, err
			}
			for _, message := range messages {
				if message.Preview.ContentHash == "" {
					continue
				}
				ref := mail.SourceRef{Revision: account.Revision, Kind: "imap", AccountID: account.ID, Mailbox: message.Mailbox, UID: message.UID, Validity: message.Validity}
				p := message.Preview
				p.Text = summary(p.Text)
				r := Record{Identity: identityFor(account.ID, account.Revision, p, ref), Ref: ref, AccountName: account.Name, AccountAddress: account.Address, Preview: p, At: message.Received, Own: strings.EqualFold(account.Address, p.Sender)}
				seen := false
				for _, flag := range message.Flags {
					if flag == `\Seen` {
						seen = true
					}
				}
				r.Unread = !seen
				if old, ok := records[r.Identity.Key]; ok {
					r.Unread = r.Unread || old.Unread
					if old.Ref.Mailbox == "INBOX" && r.Ref.Mailbox != "INBOX" {
						old.Unread = r.Unread
						records[r.Identity.Key] = old
						continue
					}
				}
				records[r.Identity.Key] = r
			}
		}
		if s.outbox != nil {
			outgoing, err := s.outbox.Sources(ctx, owner, item.ID)
			if err != nil {
				return nil, err
			}
			for _, source := range outgoing {
				p := mailsync.Parse(source.Raw)
				p.Text = summary(p.Text)
				ref := mail.SourceRef{Revision: account.Revision, Kind: "outbox", AccountID: account.ID, OutgoingID: source.Record.ID}
				key := identityFor(account.ID, account.Revision, p, ref)
				if existing, ok := records[key.Key]; ok {
					existing.State = source.Record.State
					existing.CopyState = source.Record.CopyState
					records[key.Key] = existing
					continue
				}
				records[key.Key] = Record{Identity: key, Ref: ref, AccountName: account.Name, AccountAddress: account.Address, Preview: p, At: source.Record.CreatedAt, Own: true, State: source.Record.State, CopyState: source.Record.CopyState}
			}
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	result := make([]Record, 0, len(records))
	for _, r := range records {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].At.Equal(result[j].At) {
			return result[i].Identity.Key < result[j].Identity.Key
		}
		return result[i].At.After(result[j].At)
	})
	if len(result) > 1000 {
		result = result[:1000]
	}
	return result, nil
}
func summary(value string) string {
	runes := []rune(value)
	if len(runes) > 1000 {
		return string(runes[:1000]) + "…"
	}
	return strings.Clone(value)
}
