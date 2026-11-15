// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/timeline"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func historyData(count int) TimelineData {
	d := TimelineData{UserID: "owner", Timezone: "Europe/Warsaw", Filter: "all", Total: count, Unread: count, Unsorted: count}
	for i := 0; i < count; i++ {
		d.Messages = append(d.Messages, TimelineMessage{Record: timeline.Record{Identity: mail.MessageIdentity{Key: fmt.Sprintf("%064x", i+1)}, At: time.Unix(int64(i), 0)}})
	}
	return d
}
func TestHistoryLatestTwentyAndStableOlderBoundaries(t *testing.T) {
	d := historyData(65)
	all := append([]TimelineMessage(nil), d.Messages...)
	if err := PageTimeline(&d); err != nil {
		t.Fatal(err)
	}
	if len(d.Messages) != 20 || d.Messages[0].Identity.Key != all[45].Identity.Key || !d.HasOlder || d.HasNewer || d.Total != 65 || d.Unread != 65 || d.Unsorted != 65 {
		t.Fatal("presentation window changed counts or newest ordering")
	}
	if !d.Messages[0].ShowDay || !d.Messages[0].StartsRun {
		t.Fatal("first visible record lacks context")
	}
	cursor := d.NextBefore
	// Newer arrivals cannot shift a cursor's older boundary.
	next := historyData(70)
	next.HistoryBefore = cursor
	if err := PageTimeline(&next); err != nil {
		t.Fatal(err)
	}
	if len(next.Messages) != 20 || next.Messages[0].Identity.Key != all[25].Identity.Key || next.Messages[19].Identity.Key != all[44].Identity.Key {
		t.Fatal("newer arrivals shifted older page")
	}
	seen := map[string]bool{}
	for _, m := range append(d.Messages, next.Messages...) {
		if seen[m.Identity.Key] {
			t.Fatal("page duplicated a key")
		}
		seen[m.Identity.Key] = true
	}
	end := historyData(65)
	end.HistoryBefore = historyToken(end, all[5].Identity.Key)
	if err := PageTimeline(&end); err != nil || len(end.Messages) != 5 || end.HasOlder || end.NextBefore != "" {
		t.Fatal("oldest bounded page wrong", err)
	}
	empty := historyData(0)
	if err := PageTimeline(&empty); err != nil || empty.HasOlder || len(empty.Messages) != 0 {
		t.Fatal("empty history wrong")
	}
}
func TestHistoryCursorScopesAndUnknownValuesFailClosed(t *testing.T) {
	original := historyData(40)
	token := historyToken(original, original.Messages[20].Identity.Key)
	for _, change := range []func(*TimelineData){func(d *TimelineData) { d.UserID = "foreign" }, func(d *TimelineData) { d.AccountID = "foreign-account" }, func(d *TimelineData) { d.Filter = "unread" }, func(d *TimelineData) { d.SpaceID = "space" }, func(d *TimelineData) { d.LabelID = "label" }, func(d *TimelineData) { d.Person = "person@example.test" }, func(d *TimelineData) { d.Query = "subject:invoice" }, func(d *TimelineData) { d.Timezone = "UTC" }, func(d *TimelineData) { d.Day = "2026-11-15" }} {
		d := historyData(40)
		d.HistoryBefore = token
		change(&d)
		if PageTimeline(&d) == nil {
			t.Fatal("cursor escaped its view")
		}
	}
	raw, _ := base64.RawURLEncoding.DecodeString(token)
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"unknown", "v", "anchor"} {
		copy := map[string]any{}
		for k, v := range fields {
			copy[k] = v
		}
		copy[key] = "invalid"
		data, _ := json.Marshal(copy)
		d := historyData(40)
		d.HistoryBefore = base64.RawURLEncoding.EncodeToString(data)
		if PageTimeline(&d) == nil {
			t.Fatal("unknown cursor value accepted")
		}
	}
	d := historyData(40)
	d.HistoryBefore = historyToken(d, fmt.Sprintf("%064x", 999))
	if PageTimeline(&d) == nil {
		t.Fatal("missing/stale anchor fell back to another page")
	}
	d = historyData(40)
	d.HistoryBefore = strings.Repeat("x", 513)
	if PageTimeline(&d) == nil {
		t.Fatal("cursor bound ignored")
	}
}
func TestHistorySelectedReplyAndThreadPresentation(t *testing.T) {
	d := historyData(80)
	selected := d.Messages[4]
	d.Reply = &selected
	if err := PageTimeline(&d); err != nil || len(d.Messages) != 20 || !d.HasNewer {
		t.Fatal("selected original window wrong", err)
	}
	found := false
	for _, m := range d.Messages {
		found = found || m.Identity.Key == selected.Identity.Key
	}
	if !found {
		t.Fatal("selected original disappeared")
	}
	d = historyData(80)
	d.ThreadKey = "thread"
	if err := PageTimeline(&d); err != nil || len(d.Messages) != 80 || d.HistoryEnabled {
		t.Fatal("paging silently changed thread scope")
	}
}
func TestHistoryFragmentHasRowsAndSafeNativeFallbackOnly(t *testing.T) {
	d := historyData(40)
	if err := PageTimeline(&d); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if err := RenderTimelineHistory(response, false, d); err != nil {
		t.Fatal(err)
	}
	body := response.Body.String()
	if strings.Count(body, "data-history-row") != 20 || !strings.Contains(body, `hx-params="none"`) || !strings.Contains(body, `hx-target="#timeline-history"`) || !strings.Contains(body, `href="/?before=`) {
		t.Fatal("fragment or native bounded page missing")
	}
	for _, forbidden := range []string{"<html", `id="timeline-shell"`, `id="inline-composer"`, "data-reply-body", "type=\"password\""} {
		if strings.Contains(body, forbidden) {
			t.Fatal("history response replaces private app controls", forbidden)
		}
	}
	if strings.Contains(d.Link("filter", "unread"), "before=") || strings.Contains(d.NewURL(), "before=") {
		t.Fatal("new view inherited older cursor")
	}
	m := d.Messages[0]
	m.ReturnURL = d.OlderURL()
	if strings.Contains(m.ThreadURL(), "before=") {
		t.Fatal("thread navigation inherited root cursor")
	}
}
