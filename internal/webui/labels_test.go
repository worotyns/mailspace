// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"net/http/httptest"
	"strings"
	"testing"

	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/mail"
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
