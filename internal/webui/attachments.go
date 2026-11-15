// SPDX-License-Identifier: AGPL-3.0-only
package webui

import "net/http"

// AttachmentRow is one attachment reference in the cross-message view. Bytes
// stay in the cached original message; the row only links to the reader.
type AttachmentRow struct {
	Token    string
	Name     string
	Type     string
	Subject  string
	Sender   string
	Account  string
	Time     string
	SharedBy string
	// Meta is the pre-joined metadata line, kept contiguous in the DOM.
	Meta       string
	ReadURL    string
	SharedRead bool
}

type AttachmentsData struct {
	CSRF      string
	Query     string
	Total     int
	Rows      []AttachmentRow
	EmptyText string
}

func RenderAttachments(w http.ResponseWriter, dev bool, data AttachmentsData) error {
	return renderTimelinePart(w, dev, "attachments-page", data)
}
