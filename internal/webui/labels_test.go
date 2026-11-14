// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"net/http/httptest"
	"strings"
	"testing"

	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/timeline"
)

func TestMessageLabelsReplaceMetadataOnlyAndKeepPickerOpen(t *testing.T) {
	todo := CollectionView{Item: collections.Item{ID: "todo", Kind: "label", Name: "Todo", Color: "blue"}}
	done := CollectionView{Item: collections.Item{ID: "done", Kind: "label", Name: "Done <script>", Color: "green"}}
	message := TimelineMessage{Record: timeline.Record{Identity: mail.MessageIdentity{Key: "one"}}, LabelsOpen: true, LabelsFocusID: "todo", Labels: []CollectionView{todo, done}, Memberships: []CollectionView{todo}, ReturnURL: "/?label=todo", CSRF: "test", Token: "reference"}
	sidebar := TimelineData{Labels: []CollectionView{todo, done}, LabelID: "todo"}
	response := httptest.NewRecorder()
	if err := RenderMessageLabels(response, false, message, sidebar); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	for _, want := range []string{`id="labels-message-one"`, `open`, `hx-target="#labels-message-one"`, `aria-pressed="true"`, `autofocus`, `aria-pressed="false"`, `name="kind"`, `value="label"`, `hx-swap-oob="outerHTML"`, `id="sidebar-labels"`, `Done &lt;script&gt;`} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %s", want, html)
		}
	}
	for _, forbidden := range []string{"data-inline-plaintext", "timeline-membership", "<script>", "inline-composer"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("metadata update contains %q", forbidden)
		}
	}
	drawer := message
	drawer.ReturnURL = "/?thread=one"
	if drawer.LabelsID() == message.LabelsID() {
		t.Fatal("drawer and feed must not share IDs")
	}
}

func TestLabelBadgesLiveInEveryMessageHeader(t *testing.T) {
	label := CollectionView{Item: collections.Item{ID: "todo", Kind: "label", Name: "Todo", Color: "blue"}}
	message := TimelineMessage{Record: timeline.Record{Identity: mail.MessageIdentity{Key: "grouped"}}, Labels: []CollectionView{label}, Memberships: []CollectionView{label}, ReturnURL: "/"}
	// A grouped continuation has no repeated sender but still needs its own labels.
	data := TimelineData{Messages: []TimelineMessage{message}, Labels: []CollectionView{label}}
	response := httptest.NewRecorder()
	if err := RenderTimeline(response, httptest.NewRequest("GET", "/", nil), false, data); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	labelAt := strings.Index(html, `id="labels-message-grouped"`)
	footerAt := strings.Index(html, `id="membership-grouped"`)
	if labelAt < 0 || footerAt < labelAt {
		t.Fatal("grouped message has no independent header labels")
	}
	if !strings.Contains(html, `aria-label="Labels"`) || !strings.Contains(html, `href="/?label=todo"`) {
		t.Fatal("label quick filter missing")
	}
	footer := httptest.NewRecorder()
	if err := RenderMembership(footer, false, message); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(footer.Body.String(), "Todo") {
		t.Fatal("label badges duplicated in message footer")
	}
}

func TestSidebarLabelsStartUserDefinedQuickFilter(t *testing.T) {
	data := TimelineData{AccountID: "account-one", Filter: "unsorted", SpaceID: "space-one", Person: "someone@example.test", Query: "stale", ThreadKey: "thread-one"}
	if got := data.SidebarLabelLink("custom-status"); got != "/?account=account-one&label=custom-status" {
		t.Fatalf("unexpected label link %s", got)
	}
}

func TestThreadFooterOpensComposerForSelectedOriginal(t *testing.T) {
	data := TimelineData{ThreadKey: "original-key", SpaceID: "project"}
	want := "/?reply=original-key&space=project&thread=original-key#thread-composer"
	if got := data.ThreadReplyURL(); got != want {
		t.Fatalf("unexpected target %q", got)
	}
	request := httptest.NewRequest("GET", "/?thread=original-key", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Target", "thread-content")
	response := httptest.NewRecorder()
	if err := RenderTimeline(response, request, false, data); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	for _, expected := range []string{`Reply in this thread…`, `hx-target="#thread-content"`, `hx-swap="innerHTML"`, `reply=original-key`, `#thread-composer`} {
		if !strings.Contains(html, expected) {
			t.Fatalf("missing %s", expected)
		}
	}
	if strings.Contains(html, "Reply to a message above") {
		t.Fatal("thread still displays inert generic footer")
	}
}
func TestLocalizedReplyPrefixIsOnlyAPresentationLabel(t *testing.T) {
	for _, subject := range []string{"Siema", "Odp: Siema", "Re: Odp: Siema"} {
		message := TimelineMessage{Record: timeline.Record{Preview: mailsync.Preview{Subject: subject}}}
		if got := message.TopicLabel(); got != "Siema" {
			t.Fatalf("label for %q: %q", subject, got)
		}
	}
}
