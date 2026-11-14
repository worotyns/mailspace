// SPDX-License-Identifier: AGPL-3.0-only
package mailfilter

import (
	"strings"
	"testing"
)

func TestBooleanFiltersAndPrivacy(t *testing.T) {
	message := Message{From: "sender@example.test", To: []string{"team@example.test"}, Subject: "November invoice", Text: "Payment for WDFT", Name: "Synthetic Sender"}
	for query, want := range map[string]bool{
		"November invoice": true,
		`(subject:invoice OR subject:receipt) AND body:wdft`:                    true,
		`from:other@example.test OR subject:invoice AND body:unrelated`:         false,
		`(from:other@example.test OR subject:invoice) AND to:team@example.test`: true,
		`domain:example.test body:"Payment for"`:                                true,
		`from:example.test`:                                                     false,
	} {
		expr, err := Compile(query)
		if err != nil || expr.Match(message) != want {
			t.Fatal(query, want, err)
		}
	}
	encrypted := message
	encrypted.Encrypted = true
	expr, _ := Compile(`body:wdft`)
	if expr.Match(encrypted) {
		t.Fatal("encrypted body matched")
	}
	expr, _ = Compile(`subject:invoice`)
	if !expr.Match(encrypted) {
		t.Fatal("metadata search unnecessarily blocked")
	}
	message.Text = strings.Repeat("x", 65536) + "SECRET_AFTER_LIMIT"
	expr, _ = Compile(`body:SECRET_AFTER_LIMIT`)
	if expr.Match(message) {
		t.Fatal("body scan unbounded")
	}
}
func TestRejectMalformedAndExcessiveFilters(t *testing.T) {
	for _, query := range []string{`subject:"unclosed`, `(body:x`, `body:x OR`, `AND body:x`, `body:`, `unknown:x OR subject:y`, strings.Repeat("(", 5) + "body:x" + strings.Repeat(")", 5), strings.Repeat("body:x AND ", 16) + "body:x", "body:x\r\nbody:y"} {
		if _, err := Compile(query); err == nil {
			t.Fatal("accepted", query)
		}
	}
}

func TestRuleDraftRoundTripsExactSearchMeaning(t *testing.T) {
	messages := []Message{{Subject: "Monthly invoice", From: "sender@example.test"}, {Subject: "invoice monthly", From: "other@example.test"}, {Subject: "A AND B", Text: "quoted phrase"}}
	for _, query := range []string{`monthly invoice`, `"Monthly invoice"`, `(from:sender@example.test OR subject:monthly) AND body:"quoted phrase"`, `subject:"A AND B"`} {
		original, err := Compile(query)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := Compile(original.String())
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range messages {
			if original.Match(message) != saved.Match(message) {
				t.Fatal("rule draft changed filter meaning", query)
			}
		}
	}
}

func TestMaximumDepthSearchRoundTripsWithoutExtraRootGroup(t *testing.T) {
	query := `subject:a OR (subject:b AND (subject:c OR (subject:d AND (subject:e OR subject:f))))`
	expr, err := Compile(query)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := Compile(expr.String())
	if err != nil {
		t.Fatal("valid search became an invalid draft", expr.String(), err)
	}
	for _, subject := range []string{"a", "b c", "b d e", "b d f", "unrelated"} {
		message := Message{Subject: subject}
		if expr.Match(message) != draft.Match(message) {
			t.Fatal("draft changed grouping", subject)
		}
	}
}
func TestLongTextConditionIsBoundedByWholeExpression(t *testing.T) {
	phrase := strings.Repeat("x", 500)
	expr, err := Compile(`subject:"` + phrase + `"`)
	if err != nil {
		t.Fatal("valid bounded condition rejected", err)
	}
	if !expr.Match(Message{Subject: phrase}) {
		t.Fatal("long phrase did not match")
	}
	if _, err := Compile(`subject:"` + strings.Repeat("x", 2048) + `"`); err == nil {
		t.Fatal("overall query limit bypassed")
	}
}
