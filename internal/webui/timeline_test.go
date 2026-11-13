// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/timeline"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTimelineRendersIndividualSourcesAndLocalEncryptionControls(t *testing.T) {
	records := []timeline.Record{{Identity: mail.MessageIdentity{Key: "second"}, Ref: mail.SourceRef{Kind: "imap", AccountID: "account", Revision: 1, Mailbox: "INBOX", Validity: 3, UID: 2}, AccountAddress: "me@example.test", Preview: mailsync.Preview{Sender: "sender@example.test", Subject: "<script>hostile</script>", Encrypted: true}, At: time.Unix(2000, 0)}, {Identity: mail.MessageIdentity{Key: "first"}, Preview: mailsync.Preview{Sender: "sender@example.test"}, At: time.Unix(1000, 0)}}
	data := TimelineData{Email: "me@example.test", CSRF: "csrf", Timezone: "UTC", SpaceID: "one"}
	FormatTimeline(records, []collections.Item{{ID: "one", Kind: "space", Name: "Work", Color: "blue"}, {ID: "two", Kind: "space", Name: "Personal"}, {ID: "three", Kind: "space", Name: "Other"}}, nil, map[string][]string{"second": {"one", "two"}, "first": {"two"}}, &data)
	if len(data.Messages) != 1 || len(data.Messages[0].Memberships) != 2 {
		t.Fatal("Space must filter individual messages and retain many memberships")
	}
	data.Messages[0].Expanded = true
	selected := data.Messages[0]
	data.Reply = &selected
	response := httptest.NewRecorder()
	if err := RenderTimeline(response, httptest.NewRequest("GET", "/timeline", nil), false, data); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	for _, text := range []string{"inline-key-dialog", "inline-key-passphrase", "timeline-ui.mjs", "&lt;script&gt;hostile&lt;/script&gt;", "data-encryption-required=\"true\""} {
		if !strings.Contains(html, text) {
			t.Fatalf("missing %s", text)
		}
	}
	selections := regexp.MustCompile(`<input[^>]*name="selected"[^>]*value="([01])"[^>]*>`).FindAllStringSubmatch(html, -1)
	zero, one := 0, 0
	for _, match := range selections {
		if match[1] == "0" {
			zero++
		} else {
			one++
		}
	}
	if zero != 2 || one != 1 {
		t.Fatal("membership actions changed by template formatting", zero, one)
	}

	if strings.Contains(html, "<script>hostile") || strings.Contains(html, `id="inline-key-passphrase" name=`) {
		t.Fatal("unsafe mail or named private control")
	}
	start := strings.Index(html, `id="inline-reply-body"`)
	end := strings.Index(html[start:], ">")
	if strings.Contains(html[start:start+end], `name="body"`) {
		t.Fatal("encrypted parent body is serializable")
	}
}

func TestDialogRunsAndTimezoneKeepSourcesSeparate(t *testing.T) {
	base := time.Date(2026, 11, 1, 0, 30, 0, 0, time.UTC)
	record := func(key, id, sender string, at time.Time, refs ...string) timeline.Record {
		return timeline.Record{Identity: mail.MessageIdentity{Key: key}, Ref: mail.SourceRef{AccountID: "account", Revision: 1}, At: at, Preview: mailsync.Preview{MessageID: id, Sender: sender, References: refs}}
	}
	data := TimelineData{Timezone: "America/New_York", Day: "2026-10-31"}
	records := []timeline.Record{record("third", "c", "other@example.test", base.Add(2*time.Minute), "a"), record("second", "b", "same@example.test", base.Add(time.Minute), "a"), record("first", "a", "same@example.test", base)}
	FormatTimeline(records, nil, nil, nil, &data)
	if len(data.Messages) != 3 || data.Messages[0].Time != "20:30" {
		t.Fatal("timezone or individual source lost")
	}
	if !data.Messages[0].StartsRun || data.Messages[1].StartsRun || data.Messages[1].StartsTopic || !data.Messages[2].StartsRun || data.Messages[2].StartsTopic {
		t.Fatal("dialog grouping incorrect")
	}
	data.ThreadKey = "first"
	if strings.Contains(data.Link("space", "other"), "thread=") {
		t.Fatal("Space navigation trapped inside thread")
	}
	if strings.Contains(data.NewURL(), "thread=") {
		t.Fatal("new email inherited thread")
	}

	link := data.Link("reply", "second")
	if !strings.Contains(link, "thread=first") || !strings.Contains(link, "reply=second") || strings.Contains(link, "/timeline") {
		t.Fatal("thread reply context lost")
	}
}

func TestTriageExcludesOwnMailAndKeepsAllActivity(t *testing.T) {
	incoming := timeline.Record{Identity: mail.MessageIdentity{Key: "incoming"}, Preview: mailsync.Preview{Status: "ready", Sender: "sender@example.test"}}
	own := timeline.Record{Identity: mail.MessageIdentity{Key: "own"}, Own: true, Preview: mailsync.Preview{Status: "ready"}}
	space := collections.Item{ID: "space", Kind: "space", Name: "Payments"}
	data := TimelineData{Filter: "unsorted", Timezone: "UTC"}
	FormatTimeline([]timeline.Record{incoming, own}, []collections.Item{space}, nil, map[string][]string{"incoming": {"space"}}, &data)
	if data.Unsorted != 0 || len(data.Messages) != 0 {
		t.Fatal("assigned/own messages kept triage nonempty")
	}
	all := TimelineData{Filter: "all", Timezone: "UTC"}
	FormatTimeline([]timeline.Record{incoming, own}, []collections.Item{space}, nil, map[string][]string{"incoming": {"space"}}, &all)
	if len(all.Messages) != 2 {
		t.Fatal("inbox zero hid retained activity")
	}
}
