// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mailspace.local/mailspace/internal/mail"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Conversations links only unambiguous Message-ID references within one account
// and folder. Subject similarity never joins threads; duplicate IDs stay separate.
func Conversations(messages []CachedMessage) []mail.Conversation {

	nodes := make([]mail.ThreadNode, len(messages))
	for i, m := range messages {
		scope := fmt.Sprintf("%s\x00%s\x00%d", m.AccountID, m.Mailbox, m.Validity)
		nodes[i] = mail.ThreadNode{Scope: scope, MessageID: m.Preview.MessageID, References: m.Preview.References}
	}
	parents := mail.ReferenceGroups(nodes)
	groups := map[int][]int{}
	for i := range messages {
		root := parents[i]
		groups[root] = append(groups[root], i)
	}
	result := []mail.Conversation{}
	dates := map[string]int64{}
	for _, indices := range groups {
		sort.Slice(indices, func(i, j int) bool {
			a, b := messages[indices[i]], messages[indices[j]]
			if a.Received.Equal(b.Received) {
				return a.UID < b.UID
			}
			return a.Received.Before(b.Received)
		})
		first := messages[indices[0]]
		identity := fmt.Sprintf("%s\x00%d\x00%d", first.AccountID+"\x00"+first.Mailbox, first.Validity, first.UID)
		hash := sha256.Sum256([]byte(identity))
		c := mail.Conversation{ID: hex.EncodeToString(hash[:16]), Subject: first.Preview.Subject, Channel: first.Mailbox, Folder: first.Mailbox}
		for _, idx := range indices {
			m := messages[idx]
			p := m.Preview
			initials := []rune{}
			for _, word := range strings.Fields(p.Name) {
				r := []rune(word)
				if len(r) > 0 {
					initials = append(initials, unicode.ToUpper(r[0]))
					if len(initials) == 2 {
						break
					}
				}
			}
			if len(initials) == 0 {
				initials = []rune{'?'}
			}
			display := p.Text
			if p.Status == "pending" {
				display = "Waiting for download. Refresh to continue synchronization."
			}
			if p.Status == "too-large" {
				display = "Message exceeds the 4 MiB preview limit. It remains on your mail server."
			}
			c.Messages = append(c.Messages, mail.Message{Timestamp: m.Received.UTC().Format(time.RFC3339), Locator: &mail.Locator{Mailbox: m.Mailbox, UIDValidity: m.Validity, UID: m.UID}, ID: fmt.Sprint(m.Validity, "-", m.UID), Sender: p.Sender, Name: p.Name, Initials: string(initials), Tone: []string{"lilac", "green", "peach", "blue", "rose"}[m.UID%5], Time: m.Received.Format("02 Jan 15:04"), Text: display, Encrypted: p.Encrypted})
			seen := false
			for _, f := range m.Flags {
				if f == `\Seen` {
					seen = true
				}
				if f == `\Flagged` {
					c.Starred = true
				}
			}
			if !seen {
				c.Unread = true
			}
		}
		dates[c.ID] = messages[indices[len(indices)-1]].Received.Unix()
		result = append(result, c)
	}
	// Use actual timestamps rather than presentation strings across years/months.
	sort.SliceStable(result, func(i, j int) bool {
		if dates[result[i].ID] == dates[result[j].ID] {
			return result[i].ID < result[j].ID
		}
		return dates[result[i].ID] > dates[result[j].ID]
	})
	return result
}
