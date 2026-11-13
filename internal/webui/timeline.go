// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/decision"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/timeline"
	"net/http"
	netmail "net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type CollectionView struct {
	collections.Item
	Active bool
	Count  int
}

func (c CollectionView) Tone() string {
	for _, color := range spacePalette {
		if color.Value == c.Color {
			return color.Tone
		}
	}
	return spacePalette[0].Tone
}

type PersonView struct {
	Name  string
	Email string
	Count int
}
type TimelineMessage struct {
	Classifier bool
	Archived   bool
	Decision   *DecisionView
	timeline.Record
	Token       string
	Day         string
	Time        string
	ShowDay     bool
	StartsRun   bool
	StartsTopic bool
	Expanded    bool
	Selected    bool
	Memberships []CollectionView
	Spaces      []CollectionView
	Labels      []CollectionView
	ReturnURL   string
	CSRF        string
}

func (m TimelineMessage) Initials() string {
	value := m.Preview.Name
	if value == "" {
		value = m.Preview.Sender
	}
	var result []rune
	for _, word := range strings.Fields(value) {
		for _, r := range word {
			result = append(result, unicode.ToUpper(r))
			break
		}
		if len(result) == 2 {
			break
		}
	}
	if len(result) == 0 {
		return "?"
	}
	return string(result)
}
func (m TimelineMessage) Sender() string {
	if m.Own {
		return "You"
	}
	if m.Preview.Name != "" {
		return m.Preview.Name
	}
	return m.Preview.Sender
}
func (m TimelineMessage) Recipient() string {
	if !m.Own {
		if m.Preview.ReplyTo != "" {
			return m.Preview.ReplyTo
		}
		return m.Preview.Sender
	}
	for _, to := range m.Preview.To {
		if !strings.EqualFold(to, m.AccountAddress) {
			return to
		}
	}
	return ""
}
func (m TimelineMessage) Subject() string {
	subject := m.Preview.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	if len(subject) > 200 {
		subject = subject[:197]
		for !utf8.ValidString(subject) {
			subject = subject[:len(subject)-1]
		}
		subject += "…"
	}
	return subject
}
func (m TimelineMessage) ReadURL() string {
	return "/messages/read?" + url.Values{"reference": {m.Token}}.Encode()
}
func (m TimelineMessage) Has(id string) bool {
	for _, collection := range m.Memberships {
		if collection.ID == id {
			return true
		}
	}
	return false
}
func (m TimelineMessage) ReplyURL() string {
	parsed, _ := url.Parse(m.ReturnURL)
	q := parsed.Query()
	q.Set("reply", m.Identity.Key)
	q.Del("compose")
	parsed.RawQuery = q.Encode()
	if q.Get("thread") != "" {
		return parsed.String() + "#thread-composer"
	}
	return parsed.String() + "#inline-composer"
}

type DecisionView struct {
	ID, State  string
	Confidence int
	Names      []string
}

func DecisionSummary(run decision.Run, spaces []collections.Item) *DecisionView {
	if run.State != "dry-run" && run.State != "review" && run.State != "applied" {
		return nil
	}
	d := &DecisionView{ID: run.ID, State: run.State, Confidence: int(run.Proposal.Confidence*100 + .5)}
	for _, id := range run.Proposal.SpaceIDs {
		for _, space := range spaces {
			if space.ID == id {
				d.Names = append(d.Names, space.Name)
			}
		}
	}
	return d
}

type TimelineData struct {
	FromDate, UntilDate string

	Archived                  map[string]bool
	Decisions                 map[string]decision.Run
	PeopleSearch              string
	PeopleOffset, PeopleTotal int
	PeoplePage                []PersonView

	Classifier     bool
	Provider       string
	Email          string
	UserID         string
	CSRF           string
	SessionExpires int64
	WorkspaceName  string
	Synthetic      bool
	AccountID      string
	Accounts       []AccountSummary
	Spaces         []CollectionView
	Labels         []CollectionView
	People         []PersonView
	ThreadKey      string
	SpaceID        string
	LabelID        string
	Person         string
	Filter         string
	Query          string
	Day            string
	Timezone       string
	Notice         string
	Title          string
	Description    string
	Messages       []TimelineMessage
	Reply          *TimelineMessage
	ComposeNew     bool
	Nonce          string
	OwnAddress     string
	Total          int
	Unsorted       int
	Unread         int
	Icons          template.HTML
}

func (d TimelineData) Link(key, value string) string {
	q := url.Values{}
	for k, v := range map[string]string{"space": d.SpaceID, "label": d.LabelID, "person": d.Person, "filter": d.Filter, "q": d.Query, "day": d.Day, "from": d.FromDate, "until": d.UntilDate, "account": d.AccountID, "thread": d.ThreadKey} {
		if v != "" {
			q.Set(k, v)
		}
	}
	switch key {
	case "space", "label", "person", "filter", "account", "day":
		q.Del("thread")
	}
	if key == "space" || key == "filter" {
		q.Del("space")
		q.Del("filter")
		q.Del("day")
		q.Del("from")
		q.Del("until")
	}
	if key != "" {
		if value == "" {
			q.Del(key)
		} else {
			q.Set(key, value)
		}
	}
	return "/?" + q.Encode()
}
func (d TimelineData) CurrentURL() string { return d.Link("", "") }
func (d TimelineData) NewURL() string {
	d.ThreadKey = ""
	return d.Link("compose", "new") + "#inline-composer"
}
func (d TimelineData) SelectedSpace() bool { return d.SpaceID != "" }
func (d TimelineData) ActiveSpace() CollectionView {
	for _, space := range d.Spaces {
		if space.ID == d.SpaceID {
			return space
		}
	}
	return CollectionView{}
}
func EncodeSource(ref mail.SourceRef) string {
	value, _ := json.Marshal(ref)
	return base64.RawURLEncoding.EncodeToString(value)
}
func RenderTimeline(w http.ResponseWriter, r *http.Request, dev bool, data TimelineData) error {
	if data.Nonce == "" {
		data.Nonce = rand.Text()
	}
	icons, err := bundled.ReadFile("assets/icons.svg")
	if err != nil {
		return err
	}
	// This is a fixed build asset, never mail/user HTML. Inline symbols let the
	// reader keep img-src none while preserving the shared Tabler icon library.
	data.Icons = template.HTML(icons)
	templates, err := Templates(dev)
	if err != nil {
		return err
	}
	name := "timeline-page"
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true" {
		name = "timeline-shell"
		if r.Header.Get("HX-Target") == "thread-content" {
			name = "thread-content"
		}
		if r.Header.Get("HX-Target") == "timeline-feed" {
			name = "timeline-feed"
		}
	}
	var body bytes.Buffer
	if err = templates.ExecuteTemplate(&body, name, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "HX-Request, HX-History-Restore-Request, HX-Target")
	_, err = w.Write(body.Bytes())
	return err
}
func FormatTimeline(records []timeline.Record, spaces, labels []collections.Item, memberships map[string][]string, data *TimelineData) {
	location, err := time.LoadLocation(data.Timezone)
	if err != nil {
		location = time.UTC
	}
	all := map[string]CollectionView{}
	for _, item := range spaces {
		all[item.ID] = CollectionView{Item: item, Active: item.ID == data.SpaceID}
	}
	for _, item := range labels {
		all[item.ID] = CollectionView{Item: item, Active: item.ID == data.LabelID}
	}
	people := map[string]PersonView{}
	for _, record := range records {
		ids := memberships[record.Identity.Key]
		hasSpace := false
		spaceMatch := data.SpaceID == ""
		labelMatch := data.LabelID == ""
		for _, id := range ids {
			collection := all[id]
			if collection.Kind == "space" {
				hasSpace = true
			}
			if id == data.SpaceID {
				spaceMatch = true
			}
			if id == data.LabelID {
				labelMatch = true
			}
			if !data.Archived[record.Identity.Key] {
				collection.Count++
			}
			all[id] = collection
		}
		if !hasSpace && !record.Own && !data.Archived[record.Identity.Key] {
			data.Unsorted++
		}
		if record.Unread && !data.Archived[record.Identity.Key] {
			data.Unread++
		}
		if !record.Own && !data.Archived[record.Identity.Key] && record.Preview.Sender != "" {
			person := people[record.Preview.Sender]
			person.Email = record.Preview.Sender
			person.Name = record.Preview.Name
			person.Count++
			people[person.Email] = person
		}
		if data.ThreadKey == "" && ((data.Filter == "archived") != data.Archived[record.Identity.Key]) && data.Filter != "all" {
			continue
		}
		if !spaceMatch || !labelMatch || data.Filter == "unsorted" && (hasSpace || record.Own) || data.Filter == "unread" && !record.Unread {
			continue
		}
		if data.Person != "" {
			matches := strings.EqualFold(data.Person, record.Preview.Sender)
			for _, to := range record.Preview.To {
				matches = matches || strings.EqualFold(to, data.Person)
			}
			if !matches {
				continue
			}
		}
		if data.Query != "" && !strings.Contains(strings.ToLower(record.Preview.Subject+" "+record.Preview.Name+" "+record.Preview.Sender+" "+strings.Join(record.Preview.To, " ")), strings.ToLower(data.Query)) {
			continue
		}
		date := record.At.In(location).Format("2006-01-02")
		if data.Day != "" && date != data.Day {
			continue
		}
		if data.FromDate != "" {
			until := data.UntilDate
			if until == "" {
				until = data.FromDate
			}
			if date < data.FromDate || date > until {
				continue
			}
		}

		message := TimelineMessage{Archived: data.Archived[record.Identity.Key], Decision: DecisionSummary(data.Decisions[record.Identity.Key], spaces), Spaces: data.Spaces, Classifier: data.Classifier && !data.Archived[record.Identity.Key] && !record.Own && !record.Preview.Encrypted && record.Preview.Status == "ready", Record: record, Token: EncodeSource(record.Ref), Day: record.At.In(location).Format("Monday, 2 January 2006"), Time: record.At.In(location).Format("15:04"), ReturnURL: data.CurrentURL(), CSRF: data.CSRF}
		for _, id := range ids {
			if collection, ok := all[id]; ok {
				message.Memberships = append(message.Memberships, collection)
			}
		}
		data.Messages = append(data.Messages, message)
	}
	for _, item := range spaces {
		data.Spaces = append(data.Spaces, all[item.ID])
	}
	for _, item := range labels {
		data.Labels = append(data.Labels, all[item.ID])
	}
	for _, person := range people {
		data.People = append(data.People, person)
	}
	sort.Slice(data.People, func(i, j int) bool {
		if data.People[i].Count == data.People[j].Count {
			return data.People[i].Email < data.People[j].Email
		}
		return data.People[i].Count > data.People[j].Count
	})
	// Records arrive newest-first; a Space dialog reads chronologically downward.
	for i, j := 0, len(data.Messages)-1; i < j; i, j = i+1, j-1 {
		data.Messages[i], data.Messages[j] = data.Messages[j], data.Messages[i]
	}
	previous := ""
	for i := range data.Messages {
		m := &data.Messages[i]
		m.ShowDay = m.Day != previous
		m.StartsRun = true
		m.StartsTopic = true
		if i > 0 {
			before := data.Messages[i-1]
			sameTopic := m.topic() != "" && m.topic() == before.topic() && m.Ref.AccountID == before.Ref.AccountID && m.Ref.Revision == before.Ref.Revision
			m.StartsTopic = m.ShowDay || !sameTopic
			elapsed := m.At.Sub(before.At)
			m.StartsRun = m.StartsTopic || !strings.EqualFold(m.Preview.Sender, before.Preview.Sender) || m.Own != before.Own || elapsed < 0 || elapsed > 15*time.Minute
		}

		previous = m.Day
		m.Spaces = data.Spaces
		m.Labels = data.Labels
	}
	data.Total = len(data.Messages)
	matches := []PersonView{}
	for _, p := range data.People {
		if data.PeopleSearch == "" || strings.Contains(strings.ToLower(p.Name+" "+p.Email), strings.ToLower(data.PeopleSearch)) {
			matches = append(matches, p)
		}
	}
	data.PeopleTotal = len(matches)
	if data.PeopleOffset > len(matches) {
		data.PeopleOffset = len(matches)
	}
	end := data.PeopleOffset + 50
	if end > len(matches) {
		end = len(matches)
	}
	data.PeoplePage = matches[data.PeopleOffset:end]
	if len(data.People) > 6 {
		data.People = data.People[:6]
	}

}

func (d TimelineData) EncryptionRequired() bool { return d.Reply != nil && d.Reply.Preview.Encrypted }

func (d TimelineData) ExpandURL() string {
	if d.Reply != nil {
		if d.Reply.Ref.Kind != "imap" {
			return d.Reply.ReadURL()
		}
		return "/secure/compose?" + url.Values{"account": {d.Reply.Ref.AccountID}, "folder": {d.Reply.Ref.Mailbox}, "validity": {fmt.Sprint(d.Reply.Ref.Validity)}, "uid": {fmt.Sprint(d.Reply.Ref.UID)}}.Encode()
	}
	account := ""
	if len(d.Accounts) > 0 {
		account = d.Accounts[0].ID
	}
	return "/secure/compose?" + url.Values{"account": {account}, "to": {d.ComposeRecipient()}}.Encode()
}

func (m TimelineMessage) NextSelection(id string) string {
	if m.Has(id) {
		return "0"
	}
	return "1"
}

func (m TimelineMessage) ThreadURL() string {
	parsed, _ := url.Parse(m.ReturnURL)
	q := parsed.Query()
	q.Set("thread", m.Identity.Key)
	q.Del("reply")
	q.Del("compose")
	parsed.RawQuery = q.Encode()
	return parsed.String()
}
func (d TimelineData) BackToSpaceURL() string { d.ThreadKey = ""; return d.CurrentURL() }
func (m TimelineMessage) topic() string {
	if len(m.Preview.References) > 0 {
		return m.Preview.References[0]
	}
	return m.Preview.MessageID
}
func (m TimelineMessage) TopicLabel() string {
	subject := strings.TrimSpace(m.Preview.Subject)
	for strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = strings.TrimSpace(subject[3:])
	}
	if subject == "" {
		return "Untitled email"
	}
	return subject
}

func (m TimelineMessage) CollectionsURL() string {
	return "/messages/collections?" + url.Values{"reference": {m.Token}, "return_to": {m.ReturnURL}}.Encode()
}

func (m TimelineMessage) SenderRuleURL(space string) string {
	return "/rules?" + url.Values{"reference": {m.Token}, "space": {space}}.Encode()
}

func (d TimelineData) PeopleURL(offset int) string {
	u, _ := url.Parse(d.CurrentURL())
	u.Path = "/people"
	q := u.Query()
	q.Set("people_page", strconv.Itoa(offset/50))
	q.Set("people_q", d.PeopleSearch)
	u.RawQuery = q.Encode()
	return u.String()
}
func (d TimelineData) PeopleNext() string {
	if d.PeopleOffset+50 >= d.PeopleTotal {
		return ""
	}
	return d.PeopleURL(d.PeopleOffset + 50)
}
func (d TimelineData) PeoplePrevious() string {
	if d.PeopleOffset == 0 {
		return ""
	}
	return d.PeopleURL(max(0, d.PeopleOffset-50))
}
func (d TimelineData) FeedID() string {
	if d.ThreadKey != "" {
		return "thread-feed"
	}
	return "timeline-feed"
}
func (m TimelineMessage) MembershipID() string {
	u, _ := url.Parse(m.ReturnURL)
	if u.Query().Get("thread") != "" {
		return "thread-membership-" + m.Identity.Key
	}
	return "membership-" + m.Identity.Key
}

func RenderPeople(w http.ResponseWriter, r *http.Request, dev bool, data TimelineData) error {
	name := "people-page"
	if r.Header.Get("HX-Request") == "true" {
		name = "people-results"
	}
	return renderTimelinePart(w, dev, name, data)
}

func (d TimelineData) ComposerPrefix() string {
	if d.ThreadKey != "" {
		return "thread"
	}
	return "inline"
}

func (m TimelineMessage) DOMID() string {
	if strings.HasPrefix(m.MembershipID(), "thread-") {
		return "thread-message-" + m.Identity.Key
	}
	return "message-" + m.Identity.Key
}

func (d TimelineData) ComposeRecipient() string {
	address, err := netmail.ParseAddress(d.Person)
	if err != nil {
		return ""
	}
	return address.Address
}

func (d TimelineData) PeopleLink(person string) string {
	d.ThreadKey = ""
	d.Filter = ""
	d.SpaceID = ""
	d.LabelID = ""
	d.Day = ""
	d.FromDate = ""
	d.UntilDate = ""
	d.Query = ""
	return d.Link("person", person)
}
func (m TimelineMessage) RuleSpace() string {
	for _, space := range m.Spaces {
		if m.Has(space.ID) {
			return space.ID
		}
	}
	return ""
}
func (m TimelineMessage) Suggested(id string) bool {
	if m.Decision == nil {
		return false
	}
	for _, name := range m.Decision.Names {
		for _, space := range m.Spaces {
			if space.ID == id && space.Name == name {
				return true
			}
		}
	}
	return false
}

func (m TimelineMessage) ArchiveSelection() string {
	if m.Archived {
		return "0"
	}
	return "1"
}
func (d TimelineData) DateLabel() string {
	if d.UntilDate == "" || d.UntilDate == d.FromDate {
		return d.FromDate
	}
	return d.FromDate + " – " + d.UntilDate
}
func (d TimelineData) ClearDatesURL() string {
	d.Day = ""
	d.FromDate = ""
	d.UntilDate = ""
	return d.CurrentURL()
}
func (d TimelineData) FilterRuleURL() string {
	return "/rules?" + url.Values{"from_filter": {d.Person}, "query_filter": {d.Query}, "space": {d.SpaceID}}.Encode()
}

func (m TimelineMessage) LabelsURL() string {
	return "/labels/assign?" + url.Values{"scope": {"message"}, "reference": {m.Token}}.Encode()
}
