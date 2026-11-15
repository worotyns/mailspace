// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"mailspace.local/mailspace/internal/timeline"
)

const TimelinePageSize = 20

var ErrHistoryCursor = errors.New("this history view changed; return to latest messages")

type historyCursor struct {
	Version int    `json:"v"`
	Anchor  string `json:"anchor"`
	Scope   string `json:"scope"`
}

func historyScope(d TimelineData) string {
	d.HistoryBefore = ""
	sum := sha256.Sum256([]byte(d.UserID + "\x00" + d.Timezone + "\x00" + d.CurrentURL()))
	return hex.EncodeToString(sum[:])
}
func historyToken(d TimelineData, key string) string {
	raw, _ := json.Marshal(historyCursor{1, key, historyScope(d)})
	return base64.RawURLEncoding.EncodeToString(raw)
}
func historyAnchor(d TimelineData) (string, error) {
	if len(d.HistoryBefore) > 512 {
		return "", ErrHistoryCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(d.HistoryBefore)
	if err != nil {
		return "", ErrHistoryCursor
	}
	var cursor historyCursor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || decoder.Decode(new(any)) != io.EOF || cursor.Version != 1 || len(cursor.Anchor) != 64 || cursor.Scope != historyScope(d) {
		return "", ErrHistoryCursor
	}
	if _, err := hex.DecodeString(cursor.Anchor); err != nil {
		return "", ErrHistoryCursor
	}
	return cursor.Anchor, nil
}

// PageTimeline limits presentation only, after the complete authorized view has
// supplied counts, bulk scopes and selected-original reply resolution.
func PageTimeline(d *TimelineData) error {
	if d.ThreadKey != "" {
		if d.HistoryBefore != "" {
			return ErrHistoryCursor
		}
		return nil
	}
	d.HistoryEnabled = true
	d.NextBefore = ""
	end := len(d.Messages)
	if d.HistoryBefore != "" {
		anchor, err := historyAnchor(*d)
		if err != nil {
			return err
		}
		found := false
		for i := range d.Messages {
			if d.Messages[i].Identity.Key == anchor {
				end = i
				found = true
				break
			}
		}
		if !found {
			return timeline.ErrNotFound
		}
	} else if d.Reply != nil {
		for i := range d.Messages {
			if d.Messages[i].Identity.Key == d.Reply.Identity.Key && i < end-TimelinePageSize {
				end = min(end, max(TimelinePageSize, i+TimelinePageSize/2+1))
				break
			}
		}
	}
	start := max(0, end-TimelinePageSize)
	d.HasOlder = start > 0
	d.HasNewer = end < len(d.Messages)
	d.Messages = d.Messages[start:end]
	if len(d.Messages) > 0 {
		// Every independently loaded batch starts with visible author/day context.
		d.Messages[0].ShowDay = true
		d.Messages[0].StartsRun = true
		d.Messages[0].StartsTopic = true
		if d.HasOlder {
			d.NextBefore = historyToken(*d, d.Messages[0].Identity.Key)
		}
	}
	return nil
}
func (d TimelineData) OlderURL() string { return d.Link("before", d.NextBefore) }
func (d TimelineData) HistoryURL() string {
	return "/timeline/history" + strings.TrimPrefix(d.OlderURL(), "/")
}
func (d TimelineData) LatestURL() string { return d.Link("before", "") }
func RenderTimelineHistory(w http.ResponseWriter, dev bool, d TimelineData) error {
	return renderTimelinePart(w, dev, "timeline-history-page", d)
}
