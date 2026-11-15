// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"fmt"
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

func TestPeoplePaginationBoundsTwentyThousandContactsAndArchiveViews(t *testing.T) {
	records := make([]timeline.Record, 20000)
	for i := range records {
		records[i] = timeline.Record{Identity: mail.MessageIdentity{Key: fmt.Sprint(i)}, Preview: mailsync.Preview{Sender: fmt.Sprintf("person%05d@example.test", i)}}
	}
	data := TimelineData{Filter: "unsorted", PeopleOffset: 50, Archived: map[string]bool{"0": true}}
	FormatTimeline(records, nil, nil, nil, &data)
	if len(data.PeoplePage) != 50 || len(data.People) != 6 || data.PeopleTotal != 19999 {
		t.Fatal("contact rendering unbounded")
	}
	if data.Unsorted != 19999 || len(data.Messages) != 19999 {
		t.Fatal("archive remained active")
	}
	archived := TimelineData{Filter: "archived", Archived: map[string]bool{"0": true}}
	FormatTimeline(records, nil, nil, nil, &archived)
	if len(archived.Messages) != 1 || !archived.Messages[0].Archived {
		t.Fatal("archive view failed")
	}
	filtered := TimelineData{PeopleSearch: "person19999"}
	FormatTimeline(records, nil, nil, nil, &filtered)
	if filtered.PeopleTotal != 1 || len(filtered.PeoplePage) != 1 {
		t.Fatal("contact search failed")
	}
	response := httptest.NewRecorder()
	if err := RenderPeople(response, httptest.NewRequest("GET", "/people", nil), false, data); err != nil {
		t.Fatal(err)
	}
	if strings.Count(response.Body.String(), "messages</span>") > 50 {
		t.Fatal("entire directory rendered")
	}
}

func TestContactComposeAndInclusiveDateRange(t *testing.T) {
	d := TimelineData{Person: "Sender <sender@example.test>"}
	if d.ComposeRecipient() != "sender@example.test" {
		t.Fatal("contact not prefilled")
	}
	d.Person = "invalid\r\nBcc: injected@example.test"
	if d.ComposeRecipient() != "" {
		t.Fatal("invalid recipient accepted")
	}
	records := []timeline.Record{}
	for day := 10; day <= 13; day++ {
		records = append(records, timeline.Record{Identity: mail.MessageIdentity{Key: fmt.Sprint(day)}, At: time.Date(2026, 11, day, 12, 0, 0, 0, time.UTC), Preview: mailsync.Preview{Sender: "sender@example.test"}})
	}
	data := TimelineData{Timezone: "UTC", FromDate: "2026-11-11", UntilDate: "2026-11-12"}
	FormatTimeline(records, nil, nil, nil, &data)
	if len(data.Messages) != 2 {
		t.Fatal("inclusive range failed", len(data.Messages))
	}
	data = TimelineData{Timezone: "UTC", FromDate: "2026-11-11"}
	FormatTimeline(records, nil, nil, nil, &data)
	if len(data.Messages) != 1 {
		t.Fatal("single day failed")
	}
}

func TestMessageHeaderShowsOneIdentityAndBareAccountAddress(t *testing.T) {
	received := TimelineMessage{StartsRun: true, Record: timeline.Record{Identity: mail.MessageIdentity{Key: "in"}, AccountName: "Mateusz Worotynski (i@worotyns.ovh)", AccountAddress: "i@worotyns.ovh", Preview: mailsync.Preview{Name: "Mateusz Worotynski", Sender: "worotyns@hotmail.com"}}}
	sent := TimelineMessage{StartsRun: true, Record: timeline.Record{Identity: mail.MessageIdentity{Key: "out"}, Own: true, AccountName: "Mateusz Worotynski (i@worotyns.ovh)", AccountAddress: "i@worotyns.ovh", Preview: mailsync.Preview{Sender: "i@worotyns.ovh", Name: "Mateusz Worotynski", To: []string{"worotyns@hotmail.com", "second@example.test", "i@worotyns.ovh"}}}}
	if got := sent.RecipientSummary(); got != "worotyns@hotmail.com +1" {
		t.Fatalf("unexpected recipient summary %q", got)
	}
	data := TimelineData{Timezone: "UTC", Messages: []TimelineMessage{received, sent}}
	response := httptest.NewRecorder()
	if err := RenderTimeline(response, httptest.NewRequest("GET", "/", nil), false, data); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	// Both directions name the identity pair, with the addresses on the second
	// line. html/template escapes the recipient count plus sign.
	for _, want := range []string{"Mateusz Worotynski", "worotyns@hotmail.com &#43;1", "worotyns@hotmail.com", ">You</span", `aria-hidden="true"`, `aria-label="Mateusz Worotynski to Mateusz Worotynski (i@worotyns.ovh)"`, "→", "i@worotyns.ovh"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	// Received: sender name -> account name, then sender address + account
	// address. Sent: You -> recipient, then account address + recipient.
	for _, visible := range []string{
		">Mateusz Worotynski</span",
		">Mateusz Worotynski (i@worotyns.ovh)</span",
		">worotyns@hotmail.com</span",
		">i@worotyns.ovh</span",
	} {
		if !strings.Contains(html, visible) {
			t.Fatalf("missing visible identity text %q", visible)
		}
	}
}

func TestMessageSeenToggleIsMetadataOnlyAndCapabilityGated(t *testing.T) {
	unread := TimelineMessage{SeenWrites: true, Record: timeline.Record{Identity: mail.MessageIdentity{Key: "one"}, Unread: true, Ref: mail.SourceRef{Kind: "imap"}, Preview: mailsync.Preview{Sender: "sender@example.test"}}, Token: "reference", CSRF: "csrf", ReturnURL: "/"}
	response := httptest.NewRecorder()
	if err := RenderMessageSeen(response, false, unread); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	for _, want := range []string{`id="seen-message-one"`, `data-message-seen`, `action="/messages/seen"`, `hx-target="#seen-message-one"`, `name="seen"`, `value="1"`, `aria-label="Unread"`, "Mark as read"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %s", want, html)
		}
	}
	for _, forbidden := range []string{"inline-composer", "data-local-preview", "textarea"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("read-state fragment contains %q", forbidden)
		}
	}
	read := unread
	read.Unread = false
	response = httptest.NewRecorder()
	if err := RenderMessageSeen(response, false, read); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Body.String(), "Mark as unread") || strings.Contains(response.Body.String(), `aria-label="Unread"`) || !strings.Contains(response.Body.String(), `value="0"`) {
		t.Fatal("read message rendered the wrong toggle", response.Body.String())
	}
	// A disabled capability keeps the unread indicator but never offers a write.
	disabled := unread
	disabled.SeenWrites = false
	response = httptest.NewRecorder()
	if err := RenderMessageSeen(response, false, disabled); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(response.Body.String(), "<form") || !strings.Contains(response.Body.String(), `aria-label="Unread"`) {
		t.Fatal("disabled capability still offered a flag write", response.Body.String())
	}
	// Outbox/sent rows have no provider flag to write and must not offer it.
	outgoing := unread
	outgoing.Ref = mail.SourceRef{Kind: "outbox", OutgoingID: "11111111-1111-1111-1111-111111111111"}
	response = httptest.NewRecorder()
	if err := RenderMessageSeen(response, false, outgoing); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(response.Body.String(), "<form") {
		t.Fatal("non-IMAP message offered a provider flag write", response.Body.String())
	}
}

func TestSpacePickerShowsAssignedBadgesAndKeepsAllExplicitToggles(t *testing.T) {
	record := timeline.Record{Identity: mail.MessageIdentity{Key: "one"}, Preview: mailsync.Preview{Sender: "sender@example.test", Status: "ready"}}
	spaces := []collections.Item{
		{ID: "current", Kind: "space", Name: "Current project", Color: "blue"},
		{ID: "other", Kind: "space", Name: "Other project"},
		{ID: "free", Kind: "space", Name: "Free project"},
	}
	memberships := map[string][]string{"one": {"current", "other"}}
	all := TimelineData{Filter: "all", Timezone: "UTC"}
	FormatTimeline([]timeline.Record{record}, spaces, nil, memberships, &all)
	if len(all.Messages) != 1 || len(all.Messages[0].SpaceBadges()) != 2 {
		t.Fatal("All activity must show every assigned Space badge")
	}
	data := TimelineData{Filter: "space", SpaceID: "current", Timezone: "UTC"}
	FormatTimeline([]timeline.Record{record}, spaces, nil, memberships, &data)
	if len(data.Messages) != 1 {
		t.Fatal("Space view lost the assigned message")
	}
	message := data.Messages[0]
	badges := message.SpaceBadges()
	if len(badges) != 1 || badges[0].ID != "other" {
		t.Fatalf("inside a Space only other assigned Spaces are badged: %#v", badges)
	}
	if message.SpacePickerID() != "spaces-message-one" || message.LabelsID() != "labels-message-one" || message.MembershipID() != "membership-one" {
		t.Fatal("scoped metadata ids changed")
	}
	response := httptest.NewRecorder()
	if err := RenderMembership(response, false, message); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	for _, want := range []string{`data-space-picker`, `data-space-options`, `id="spaces-message-one"`, `id="spaces-message-one-current"`, `id="spaces-message-one-free"`, `hx-target="#spaces-message-one"`, `action="/collections/current/membership"`, `aria-pressed="true"`, "Other project", "Free project"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %s", want)
		}
	}
	summary := html[strings.Index(html, "<summary"):strings.Index(html, "</summary>")]
	if strings.Contains(summary, "Current project") || !strings.Contains(summary, "Other project") || strings.Contains(summary, "<form") {
		t.Fatalf("compact summary must show only other assigned badges: %s", summary)
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
		t.Fatal("many-to-many toggles changed", zero, one)
	}
	fragment := httptest.NewRecorder()
	if err := RenderMessageSpaces(fragment, false, message, all); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fragment.Body.String(), `id="sidebar-spaces"`) || !strings.Contains(fragment.Body.String(), `hx-swap-oob="outerHTML"`) {
		t.Fatal("Space counts must refresh out of band without replacing the message body")
	}
}

func TestThreadPresentationAndCancelKeepFragmentScope(t *testing.T) {
	for _, thread := range []string{"", "original"} {
		data := TimelineData{ThreadKey: thread, Timezone: "UTC", Filter: "all"}
		FormatTimeline([]timeline.Record{{Identity: mail.MessageIdentity{Key: "original"}, Preview: mailsync.Preview{Sender: "sender@example.test", Text: "hello"}, At: time.Unix(1000, 0)}}, nil, nil, nil, &data)
		data.Reply = &data.Messages[0]
		request := httptest.NewRequest("GET", "/", nil)
		request.Header.Set("HX-Request", "true")
		target, swap, selection := "#timeline-shell", "outerHTML", "#timeline-shell"
		if thread != "" {
			request.Header.Set("HX-Target", "thread-content")
			target, swap, selection = "#thread-content", "innerHTML", "unset"
		}
		response := httptest.NewRecorder()
		if err := RenderTimeline(response, request, false, data); err != nil {
			t.Fatal(err)
		}
		html := response.Body.String()
		cancel := regexp.MustCompile(`(?s)<a\s+href="[^<]+?aria-label="Cancel reply"[^>]*>`).FindString(html)
		for _, attribute := range []string{`hx-target="` + target + `"`, `hx-swap="` + swap + `"`, `hx-select="` + selection + `"`} {
			if !strings.Contains(cancel, attribute) {
				t.Fatalf("cancel lost %s: %s", attribute, cancel)
			}
		}
		if thread != "" {
			for _, unwanted := range []string{`class="message-toolbar"`, `id="labels-`, `bg-canvas/95`} {
				if strings.Contains(html, unwanted) {
					t.Fatalf("thread includes %s", unwanted)
				}
			}
			if !strings.Contains(html, `Reply to sender@example.test at 00:16`) {
				t.Fatal("selected message reply unavailable")
			}
		}
	}
}

func TestOutgoingStateRemainsVisibleOutsideScrollingSpaceControls(t *testing.T) {
	for _, state := range []string{"accepted", "pending", "unknown", "rejected"} {
		data := TimelineData{Filter: "all", Timezone: "UTC"}
		FormatTimeline([]timeline.Record{{Identity: mail.MessageIdentity{Key: "sent"}, Own: true, State: state, Preview: mailsync.Preview{Sender: "me@example.test", Text: "Synthetic outgoing"}, At: time.Unix(1000, 0)}}, nil, nil, nil, &data)
		response := httptest.NewRecorder()
		if err := RenderTimeline(response, httptest.NewRequest("GET", "/", nil), false, data); err != nil {
			t.Fatal(err)
		}
		html := response.Body.String()
		badge, toolbar := strings.Index(html, `data-message-send-state="`+state+`"`), strings.Index(html, `class="message-toolbar"`)
		if badge < 0 || toolbar < 0 || badge > toolbar || strings.Count(html, `data-message-send-state=`) != 1 {
			t.Fatal("send state is hidden in or duplicated after toolbar", state)
		}
		if !strings.Contains(html[badge:toolbar], "Server acceptance is not delivery confirmation") {
			t.Fatal("misleading delivery certainty")
		}
	}
}

func TestInlineRecipientGroupsKeepNativeFormsAndScopedSuggestions(t *testing.T) {
	for _, selected := range []bool{false, true} {
		data := TimelineData{ComposeNew: true, Person: "new@example.test"}
		wantRecipient := "new@example.test"
		if selected {
			data.Reply = &TimelineMessage{Record: timeline.Record{Preview: mailsync.Preview{Sender: "original@example.test"}}}
			wantRecipient = "original@example.test"
		}
		response := httptest.NewRecorder()
		if err := renderTimelinePart(response, false, "inline-composer", data); err != nil {
			t.Fatal(err)
		}
		body := response.Body.String()
		for _, field := range []string{"to", "cc", "bcc"} {
			prefix := data.ComposerPrefix() + "-" + field
			if !strings.Contains(body, `for="`+prefix+`"`) || !strings.Contains(body, `id="`+prefix+`"`) || !strings.Contains(body, `name="`+field+`"`) || !strings.Contains(body, `list="`+prefix+`-suggestions"`) || !strings.Contains(body, `id="`+prefix+`-suggestions"`) {
				t.Fatal("recipient lacks a scoped label/control/datalist", field)
			}
		}
		if !strings.Contains(body, `value="`+wantRecipient+`"`) {
			t.Fatal("selected original recipient changed or acquired whitespace")
		}
		if !strings.Contains(body, `action="/timeline/replies/plain"`) || !strings.Contains(body, `method="post"`) || !strings.Contains(body, "Separate addresses with commas") {
			t.Fatal("native send fallback or recipient format help missing")
		}
		if strings.Contains(body, `type="email"`) {
			t.Fatal("native email input would reject display-name recipient lists")
		}
	}
}

func TestNamedContactsShowSecondaryEmailInPeopleAndSenderHeaders(t *testing.T) {
	data := TimelineData{Timezone: "UTC", Filter: "all"}
	records := []timeline.Record{
		{Identity: mail.MessageIdentity{Key: "first"}, At: time.Unix(1000, 0), Preview: mailsync.Preview{Sender: "first@example.test", Name: "Same display name"}},
		{Identity: mail.MessageIdentity{Key: "second"}, At: time.Unix(2000, 0), Preview: mailsync.Preview{Sender: "second@example.test", Name: "Same display name"}},
	}
	FormatTimeline(records, nil, nil, nil, &data)
	response := httptest.NewRecorder()
	if err := RenderTimeline(response, httptest.NewRequest("GET", "/", nil), false, data); err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile(`(?s)<small[^>]*>.*?</small`).FindAllString(response.Body.String(), -1)
	for _, address := range []string{"first@example.test", "second@example.test"} {
		count := 0
		for _, block := range blocks {
			if strings.Contains(block, address) {
				count++
			}
		}
		if count < 2 {
			t.Fatal("named contact must show its address in both sidebar and message header", address)
		}
	}
}

func TestRecipientNamesAndCcRenderWithoutInventingBcc(t *testing.T) {
	sent := TimelineMessage{StartsRun: true, Record: timeline.Record{Identity: mail.MessageIdentity{Key: "out"}, Own: true, AccountName: "Mailbox", AccountAddress: "i@worotyns.ovh", Preview: mailsync.Preview{Sender: "i@worotyns.ovh", To: []string{"worotyns@hotmail.com", "i@worotyns.ovh"}, ToRecipients: []mailsync.Recipient{{Name: "Mateusz Worotynski", Address: "worotyns@hotmail.com"}, {Address: "i@worotyns.ovh"}}}}}
	if got := sent.RecipientSummary(); got != "Mateusz Worotynski" {
		t.Fatalf("cached recipient name not used: %q", got)
	}
	received := TimelineMessage{StartsRun: true, Record: timeline.Record{Identity: mail.MessageIdentity{Key: "in"}, AccountName: "Mailbox", AccountAddress: "i@worotyns.ovh", Preview: mailsync.Preview{Name: "Sender Name", Sender: "sender@example.test", ToRecipients: []mailsync.Recipient{{Address: "i@worotyns.ovh"}}, CcRecipients: []mailsync.Recipient{{Name: "Third Person", Address: "third@example.test"}}}}}
	if got := received.CcSummary(); got != "Third Person" {
		t.Fatalf("cc summary %q", got)
	}
	if got := received.ToSummary(); got != "i@worotyns.ovh" {
		t.Fatalf("to summary %q", got)
	}
	data := TimelineData{Timezone: "UTC", Messages: []TimelineMessage{sent, received}}
	response := httptest.NewRecorder()
	if err := RenderTimeline(response, httptest.NewRequest("GET", "/", nil), false, data); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	if !strings.Contains(html, "Mateusz Worotynski") || !strings.Contains(html, "cc: Third Person") {
		t.Fatal("cached recipient names or cc missing from the row")
	}
	if strings.Contains(html, "Bcc") || strings.Contains(html, "bcc:") {
		t.Fatal("a hidden Bcc recipient was invented")
	}
}

func TestPeopleDirectoryCoversSendersAndSentRecipients(t *testing.T) {
	records := []timeline.Record{
		{Identity: mail.MessageIdentity{Key: "in"}, AccountAddress: "me@example.test", At: time.Unix(2000, 0), Preview: mailsync.Preview{Sender: "writer@example.test", Name: "Writer", ToRecipients: []mailsync.Recipient{{Address: "me@example.test"}}}},
		{Identity: mail.MessageIdentity{Key: "out"}, Own: true, AccountAddress: "me@example.test", At: time.Unix(1000, 0), Preview: mailsync.Preview{Sender: "me@example.test", To: []string{"reader@example.test"}, ToRecipients: []mailsync.Recipient{{Name: "Reader", Address: "reader@example.test"}}, CcRecipients: []mailsync.Recipient{{Address: "cc@example.test"}}}},
	}
	data := TimelineData{Filter: "all", Timezone: "UTC"}
	FormatTimeline(records, nil, nil, nil, &data)
	people := map[string]PersonView{}
	for _, person := range data.People {
		people[person.Email] = person
	}
	if people["writer@example.test"].Name != "Writer" || people["reader@example.test"].Name != "Reader" || people["cc@example.test"].Email == "" {
		t.Fatalf("directory did not cover both directions: %#v", data.People)
	}
	if _, ok := people["me@example.test"]; ok {
		t.Fatal("own account listed as a contact")
	}
}

func TestReaderShowsCachedToAndCcButNeverBcc(t *testing.T) {
	response := httptest.NewRecorder()
	data := AuthPageData{Email: "me@example.test", CSRF: "csrf", SecureRead: true, OrdinaryRead: true, ToLabel: "Mateusz Worotynski <worotyns@hotmail.com>", CcLabel: "Third Person"}
	if err := RenderAuth(response, false, "secure-mail", data); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	for _, want := range []string{">To</dt>", "Mateusz Worotynski &lt;worotyns@hotmail.com&gt;", ">Cc</dt>", "Third Person", "never invented"} {
		if !strings.Contains(html, want) {
			t.Fatalf("reader missing %q", want)
		}
	}
	if strings.Contains(html, ">Bcc</dt>") {
		t.Fatal("reader invented a Bcc line for received mail")
	}
}

func TestSharedMessageRendersReadOnlyWithoutMutationControls(t *testing.T) {
	shared := TimelineMessage{StartsRun: true, SeenWrites: true, Record: timeline.Record{Identity: mail.MessageIdentity{Key: "shared"}, Ref: mail.SourceRef{Kind: "imap"}, Shared: true, SharedBy: "alice@example.test", CanDownload: false, Preview: mailsync.Preview{Name: "Sender", Sender: "sender@example.test", ToRecipients: []mailsync.Recipient{{Address: "alice@example.test"}}}}}
	data := TimelineData{Timezone: "UTC", SeenWrites: true, Filter: "all", Messages: []TimelineMessage{shared}}
	response := httptest.NewRecorder()
	if err := RenderTimeline(response, httptest.NewRequest("GET", "/", nil), false, data); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	if !strings.Contains(html, "shared by alice@example.test") || !strings.Contains(html, `id="message-shared"`) {
		t.Fatal("shared badge missing", html)
	}
	for _, forbidden := range []string{`action="/messages/archive"`, `hx-post="/messages/seen"`, "Edit message Spaces", `href="/messages/read?`, "data-load-attachments"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("read-only shared row exposed %s", forbidden)
		}
	}
	// A download-enabled grant restores the original-message and attachment controls.
	downloadable := shared
	downloadable.CanDownload = true
	data = TimelineData{Timezone: "UTC", SeenWrites: true, Filter: "all", Messages: []TimelineMessage{downloadable}}
	response = httptest.NewRecorder()
	if err := RenderTimeline(response, httptest.NewRequest("GET", "/", nil), false, data); err != nil {
		t.Fatal(err)
	}
	html = response.Body.String()
	if !strings.Contains(html, `href="/messages/read?`) || !strings.Contains(html, "data-load-attachments") || strings.Contains(html, `action="/messages/archive"`) {
		t.Fatal("download-enabled shared row lost the read controls or gained write controls", html)
	}
}
