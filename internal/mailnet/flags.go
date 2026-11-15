// SPDX-License-Identifier: AGPL-3.0-only
package mailnet

import (
	"context"
	"errors"
	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"time"
)

// ErrFlagWrite reports a rejected or unconfirmed flag mutation. It never
// indicates whether an ambiguous remote change happened.
var ErrFlagWrite = errors.New("provider flag update failed")

// FlagWriter is a separate, bounded write boundary next to sending. It can set
// one message's \Seen flag and nothing else: no move, delete, expunge or store
// copy. The read-only session contract stays unchanged.
type FlagWriter interface {
	SetSeen(context.Context, Endpoint, Login, string, uint32, uint32, bool) error
}

// SetSeen applies only the \Seen flag to one UID. It is a fresh short-lived
// connection that selects the mailbox read-write, requires the caller's exact
// UIDVALIDITY and confirms the server's returned flags before reporting
// success. Any other outcome is an error and the caller must keep local state
// unchanged.
func (c *Connector) SetSeen(ctx context.Context, ep Endpoint, login Login, mailbox string, validity, uid uint32, seen bool) error {
	if mailbox == "" || len(mailbox) > 1024 || validity == 0 || uid == 0 {
		return ErrFlagWrite
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	session, err := c.OpenRead(ctx, ep, login)
	if err != nil {
		return err
	}
	defer session.Close()
	reader := session.(*readSession)
	selected, err := reader.client.Select(mailbox, nil).Wait()
	if err != nil || selected.UIDValidity != validity {
		reader.fail()
		return ErrFlagWrite
	}
	op := imap.StoreFlagsDel
	if seen {
		op = imap.StoreFlagsAdd
	}
	cmd := reader.client.Store(imap.UIDSetNum(imap.UID(uid)), &imap.StoreFlags{Op: op, Flags: []imap.Flag{imap.FlagSeen}}, nil)
	reader.pending = cmd
	count, confirmed := 0, false
	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		count++
		var got uint32
		var flags []imap.Flag
		for item := msg.Next(); item != nil; item = msg.Next() {
			switch v := item.(type) {
			case imapclient.FetchItemDataUID:
				got = uint32(v.UID)
			case imapclient.FetchItemDataFlags:
				flags = append(flags, v.Flags...)
			}
		}
		if got != uid || count > 1 {
			reader.fail()
			return ErrFlagWrite
		}
		matched := false
		for _, flag := range flags {
			if flag == imap.FlagSeen {
				matched = true
			}
		}
		if matched != seen {
			reader.fail()
			return ErrFlagWrite
		}
		confirmed = true
	}
	if err = cmd.Close(); err != nil {
		return reader.fail()
	}
	reader.pending = nil
	if count != 1 || !confirmed {
		return ErrFlagWrite
	}
	return nil
}
