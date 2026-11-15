// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"context"
	"strings"
)

// SearchKey identifies one cached message in the local full-text index. It is
// not an authorization capability: callers must pass only account ids the
// viewer may read.
type SearchKey struct {
	AccountID string
	Mailbox   string
	UID       uint32
}

const (
	maxSearchAccounts  = 64
	maxSearchResults   = 2000
	maxSearchTerms     = 16
	maxSearchTermRunes = 64
)

// searchExpression turns bounded user words into an FTS5 expression of quoted
// phrases combined with implicit AND. Quoting and doubling embedded quotes make
// operators and punctuation literal, never FTS syntax.
func searchExpression(query string) (string, bool) {
	terms := strings.Fields(query)
	if len(terms) > maxSearchTerms {
		terms = terms[:maxSearchTerms]
	}
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		if term == "" {
			continue
		}
		runes := []rune(term)
		if len(runes) > maxSearchTermRunes {
			term = string(runes[:maxSearchTermRunes])
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	if len(quoted) == 0 {
		return "", false
	}
	return strings.Join(quoted, " "), true
}

// SearchText matches every term against the cached ordinary preview text of the
// given authorized accounts. Encrypted mail has no indexed text and can never
// match. Results are bounded and only filter an already-authorized cached view.
func (s *Service) SearchText(ctx context.Context, accountIDs []string, query string) (map[SearchKey]bool, error) {
	result := map[SearchKey]bool{}
	expression, ok := searchExpression(query)
	if !ok {
		return result, nil
	}
	unique := make([]string, 0, maxSearchAccounts)
	seen := map[string]bool{}
	for _, id := range accountIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
		if len(unique) == maxSearchAccounts {
			break
		}
	}
	if len(unique) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(unique))
	args := make([]any, 0, len(unique)+2)
	args = append(args, expression)
	for i, id := range unique {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, maxSearchResults)
	rows, err := s.db.QueryContext(ctx, `SELECT m.account_id,m.mailbox,m.uid FROM message_search JOIN mail_messages m ON m.rowid=message_search.rowid WHERE message_search MATCH ? AND m.account_id IN (`+strings.Join(placeholders, ",")+`) LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key SearchKey
		if err = rows.Scan(&key.AccountID, &key.Mailbox, &key.UID); err != nil {
			return nil, err
		}
		result[key] = true
	}
	return result, rows.Err()
}
