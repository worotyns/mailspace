// SPDX-License-Identifier: AGPL-3.0-only
package webui

import "strings"

type MessageTextPart struct {
	Text   string
	Quoted bool
}

// QuoteParts is presentation only: preserve every byte and never alter MIME,
// reply references, signatures or the input used by classification.
func (m TimelineMessage) QuoteParts() []MessageTextPart {
	var parts []MessageTextPart
	for _, line := range strings.SplitAfter(m.Preview.Text, "\n") {
		if line == "" {
			continue
		}
		quoted := strings.HasPrefix(strings.TrimLeft(line, " \t"), ">")
		if len(parts) > 0 && parts[len(parts)-1].Quoted == quoted {
			parts[len(parts)-1].Text += line
		} else {
			parts = append(parts, MessageTextPart{Text: line, Quoted: quoted})
		}
	}
	return parts
}
