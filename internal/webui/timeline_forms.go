// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"bytes"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/outbox"
	"net/http"
)

type CollectionForm struct {
	Email string
	CSRF  string
	Item  collections.Item
	Error string
}

func (f CollectionForm) Action() string {
	prefix := "/spaces"
	if f.Item.Kind == "label" {
		prefix = "/labels"
	}
	if f.Item.ID != "" {
		prefix += "/" + f.Item.ID
	}
	return prefix
}
func (f CollectionForm) Title() string {
	if f.Item.Kind == "label" {
		return "Label"
	}
	return "Space"
}
func (d TimelineData) NewSpaceForm() CollectionForm {
	return CollectionForm{CSRF: d.CSRF, Item: collections.Item{Kind: "space", Color: "lilac"}}
}
func (d TimelineData) NewLabelForm() CollectionForm {
	return CollectionForm{CSRF: d.CSRF, Item: collections.Item{Kind: "label", Color: "rose"}}
}
func (d TimelineData) SpaceEditForm() CollectionForm {
	return CollectionForm{CSRF: d.CSRF, Item: d.ActiveSpace().Item}
}

type ReplyResult struct {
	Error     string
	Warning   string
	Record    outbox.Record
	NextURL   string
	OutboxURL string
}

func (r ReplyResult) Title() string {
	switch r.Record.State {
	case "accepted":
		return "Accepted by your mail server"
	case "rejected":
		return "Not accepted by the mail server"
	case "unknown", "sending":
		return "Send outcome uncertain — check before sending again"
	}
	return "Send attempt recorded"
}
func (r ReplyResult) Tone() string {
	if r.Record.State == "accepted" {
		return "border-success-line bg-success-soft text-success"
	}
	return "border-warning-line bg-warning-soft text-warning"
}
func renderTimelinePart(w http.ResponseWriter, dev bool, name string, data any) error {
	templates, err := Templates(dev)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	if err = templates.ExecuteTemplate(&body, name, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err = w.Write(body.Bytes())
	return err
}
func RenderCollectionForm(w http.ResponseWriter, dev bool, email, csrf string, item collections.Item, message string) error {
	return renderTimelinePart(w, dev, "collection-page", CollectionForm{Email: email, CSRF: csrf, Item: item, Error: message})
}
func RenderMembership(w http.ResponseWriter, dev bool, message TimelineMessage) error {
	return renderTimelinePart(w, dev, "timeline-membership", message)
}
func RenderReplyResult(w http.ResponseWriter, dev bool, result ReplyResult) error {
	return renderTimelinePart(w, dev, "inline-reply-result", result)
}

func RenderMembershipPage(w http.ResponseWriter, dev bool, message TimelineMessage) error {
	return renderTimelinePart(w, dev, "membership-page", message)
}
