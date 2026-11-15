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
	"mailspace.local/mailspace/internal/sharing"
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
	// Shared marks a record read from another owner's account through a grant.
	// Shared records are read-only and omit the owner's local outbox.
	// CanDownload gates the original MIME/attachment bytes separately.
	Shared      bool
	SharedBy    string
	CanDownload bool
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
	sharing  *sharing.Service
}

func New(users *identity.Store, a *accounts.Service, s *mailsync.Service, o *outbox.Service) *Service {
	return &Service{users: users, accounts: a, sync: s, outbox: o}
}

// SetSharing enables read-only access to accounts shared with the viewer. The
// owner-only List/Resolve used by rules, decisions and mutations stay unchanged.
func (s *Service) SetSharing(v *sharing.Service) { s.sharing = v }
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

// ResolveForViewer resolves an owned source, or a read-only source shared with
// the viewer through a currently valid grant. Shared outbox and non-IMAP
// sources are never exposed, and a missing or revoked grant fails closed.
func (s *Service) ResolveForViewer(ctx context.Context, viewer string, ref mail.SourceRef) (Resolved, error) {
	if resolved, err := s.Resolve(ctx, viewer, ref); err == nil {
		return resolved, nil
	}
	if s.sharing == nil || ref.Kind != "imap" {
		return Resolved{}, ErrNotFound
	}
	grant, ok, err := s.sharing.Authorize(ctx, viewer, ref.AccountID, ref.Mailbox)
	if err != nil || !ok {
		return Resolved{}, ErrNotFound
	}
	resolved, err := s.Resolve(ctx, grant.GrantorID, ref)
	if err != nil {
		return Resolved{}, ErrNotFound
	}
	resolved.Record.Shared = true
	resolved.Record.SharedBy = grant.GrantorEmail
	resolved.Record.CanDownload = grant.CanDownload
	if states, e := s.sharedUnreadStates(ctx, viewer, []string{resolved.Record.Identity.Key}); e == nil {
		if unread, ok := states[resolved.Record.Identity.Key]; ok {
			resolved.Record.Unread = unread
		}
	}
	return resolved, nil
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
		if err = s.collect(ctx, sourceScope{owner: owner, account: item.ID}, records); err != nil {
			return nil, err
		}
	}
	return finishRecords(records, found)
}

// sourceScope is one account readable by a viewer. A shared scope names the
// grantor and either the whole account or one granted folder.
type sourceScope struct {
	owner       string
	account     string
	shared      bool
	sharedBy    string
	mailbox     string
	canDownload bool
}

// ListForViewer returns the viewer's owned accounts plus accounts shared with
// them through currently valid grants. Shared scopes are read-only and never
// include the owner's local outbox. It stays a bounded cached view.
func (s *Service) ListForViewer(ctx context.Context, viewer, accountFilter string) ([]Record, error) {
	configured, err := s.users.Accounts(ctx, viewer)
	if err != nil {
		return nil, err
	}
	owned := map[string]bool{}
	scopes := []sourceScope{}
	found := accountFilter == ""
	for _, item := range configured {
		owned[item.ID] = true
		if accountFilter == "" || accountFilter == item.ID {
			scopes = append(scopes, sourceScope{owner: viewer, account: item.ID})
			found = true
		}
	}
	if s.sharing != nil {
		grants, err := s.sharing.GrantedAccounts(ctx, viewer)
		if err != nil {
			return nil, err
		}
		for _, grant := range grants {
			if owned[grant.AccountID] || accountFilter != "" && accountFilter != grant.AccountID {
				continue
			}
			scopes = append(scopes, sourceScope{owner: grant.GrantorID, account: grant.AccountID, shared: true, sharedBy: grant.GrantorEmail, mailbox: grant.Mailbox, canDownload: grant.CanDownload})
			found = true
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	records := map[string]Record{}
	seen := map[string]bool{}
	for _, scope := range scopes {
		key := scope.account + "\x00" + scope.owner + "\x00" + scope.mailbox
		if seen[key] {
			continue
		}
		seen[key] = true
		if err = s.collect(ctx, scope, records); err != nil {
			return nil, err
		}
	}
	sharedKeys := make([]string, 0, len(records))
	for _, record := range records {
		if record.Shared {
			sharedKeys = append(sharedKeys, record.Identity.Key)
		}
	}
	states, err := s.sharedUnreadStates(ctx, viewer, sharedKeys)
	if err != nil {
		return nil, err
	}
	for key, unread := range states {
		if record, ok := records[key]; ok {
			record.Unread = unread
			records[key] = record
		}
	}
	return finishRecords(records, found)
}

// maxSharedReadKeys bounds one local read-state lookup.
const maxSharedReadKeys = 2000

// sharedUnreadStates returns the viewer's own read state for shared message
// keys. Keys without a row keep the cached provider state.
func (s *Service) sharedUnreadStates(ctx context.Context, viewer string, keys []string) (map[string]bool, error) {
	result := map[string]bool{}
	unique := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, key := range keys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, key)
		if len(unique) == maxSharedReadKeys {
			break
		}
	}
	if viewer == "" || len(unique) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(unique))
	args := make([]any, 0, len(unique)+1)
	args = append(args, viewer)
	for i, key := range unique {
		placeholders[i] = "?"
		args = append(args, key)
	}
	rows, err := s.users.Database().QueryContext(ctx, `SELECT message_key,unread FROM shared_read_state WHERE user_id=? AND message_key IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var unread bool
		if err = rows.Scan(&key, &unread); err != nil {
			return nil, err
		}
		result[key] = unread
	}
	return result, rows.Err()
}

// SetSharedUnread records the viewer's local read state for one shared message.
// It never writes a provider flag or another user's state.
func (s *Service) SetSharedUnread(ctx context.Context, viewer, messageKey string, unread bool) error {
	if viewer == "" || len(messageKey) < 16 || len(messageKey) > 256 {
		return ErrInput
	}
	_, err := s.users.Database().ExecContext(ctx, `INSERT INTO shared_read_state(user_id,message_key,unread,updated_at) VALUES(?,?,?,?) ON CONFLICT(user_id,message_key) DO UPDATE SET unread=excluded.unread,updated_at=excluded.updated_at`, viewer, messageKey, unread, time.Now().Unix())
	return err
}

// collect merges one account's cached IMAP messages into records. A shared
// scope never reads the owner's outbox, and a stale account is skipped instead
// of disclosing an error.
func (s *Service) collect(ctx context.Context, scope sourceScope, records map[string]Record) error {
	account, err := s.accounts.Get(ctx, scope.owner, scope.account)
	if errors.Is(err, accounts.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	folders, err := s.sync.Folders(ctx, scope.owner, scope.account)
	if err != nil {
		return err
	}
	for _, folder := range folders {
		if scope.mailbox != "" && folder.Name != scope.mailbox {
			continue
		}
		messages, err := s.sync.Messages(ctx, scope.owner, scope.account, folder.Name)
		if err != nil {
			return err
		}
		for _, message := range messages {
			if message.Preview.ContentHash == "" {
				continue
			}
			ref := mail.SourceRef{Revision: account.Revision, Kind: "imap", AccountID: account.ID, Mailbox: message.Mailbox, UID: message.UID, Validity: message.Validity}
			p := message.Preview
			p.Text = summary(p.Text)
			r := Record{Identity: identityFor(account.ID, account.Revision, p, ref), Ref: ref, AccountName: account.Name, AccountAddress: account.Address, Preview: p, At: message.Received, Own: strings.EqualFold(account.Address, p.Sender), Shared: scope.shared, SharedBy: scope.sharedBy, CanDownload: !scope.shared || scope.canDownload}
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
	if scope.shared || s.outbox == nil {
		return nil
	}
	outgoing, err := s.outbox.Sources(ctx, scope.owner, scope.account)
	if err != nil {
		return err
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
	return nil
}

func finishRecords(records map[string]Record, found bool) ([]Record, error) {
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
