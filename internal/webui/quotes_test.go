// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/timeline"
	"strings"
	"testing"
)

func TestQuotePartsPreserveInlineRepliesAndOriginalText(t *testing.T) {
	for _, text := range []string{"", "Just a reply", "> old\n>> older\nAnswer\n  > nested\r\nAnother answer\n", "> <script>alert(1)</script>\n", "2 > 1\n"} {
		m := TimelineMessage{Record: timeline.Record{Preview: mailsync.Preview{Text: text}}}
		var original strings.Builder
		for _, part := range m.QuoteParts() {
			original.WriteString(part.Text)
		}
		if original.String() != text {
			t.Fatal("quote folding changed original message text")
		}
	}
	m := TimelineMessage{Record: timeline.Record{Preview: mailsync.Preview{Text: "> first\n>> nested\nVisible reply\n> next"}}}
	parts := m.QuoteParts()
	if len(parts) != 3 || !parts[0].Quoted || parts[1].Quoted || !parts[2].Quoted {
		t.Fatal("inline reply was hidden or nested quotes were split")
	}
}
