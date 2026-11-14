// SPDX-License-Identifier: AGPL-3.0-only
package timeline

import "mailspace.local/mailspace/internal/mail"

// Related links only unambiguous cached Message-ID references in the anchor's
// current account/configuration. It never changes Space membership or sorts by
// subject. Shared uncached ancestor references can connect descendants, but no missing message or reply target is fabricated. Identical subjects prove nothing.
func Related(records []Record, key string) ([]Record, error) {
	anchor := -1
	for i := range records {
		if records[i].Identity.Key == key {
			anchor = i
			break
		}
	}
	if anchor < 0 {
		return nil, ErrNotFound
	}
	selected := records[anchor]
	scoped := []Record{}
	for _, record := range records {
		if record.Ref.AccountID == selected.Ref.AccountID && record.Ref.Revision == selected.Ref.Revision {
			scoped = append(scoped, record)
		}
	}

	nodes := make([]mail.ThreadNode, len(scoped))
	anchor = -1
	for i, record := range scoped {
		nodes[i] = mail.ThreadNode{MessageID: record.Preview.MessageID, References: record.Preview.References}
		if record.Identity.Key == key {
			anchor = i
		}
	}
	groups := mail.ReferenceGroups(nodes)
	root := groups[anchor]
	result := []Record{}
	for i, record := range scoped {
		if groups[i] == root {
			result = append(result, record)
		}
	}
	return result, nil
}
