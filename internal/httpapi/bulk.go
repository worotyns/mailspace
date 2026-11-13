// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/timeline"
	"mailspace.local/mailspace/internal/webui"
	"net/http"
	netmail "net/mail"
	"net/url"
	"strings"
)

type selection struct {
	Scope, Key, Person, Reference, Hash string
	Refs                                []mail.SourceRef
	Total                               int
}

func (c *timelineController) selectScope(r *http.Request, s identity.Session, archive bool) (selection, error) {
	value := func(key string) string {
		if r.Method == "POST" {
			return r.FormValue(key)
		}
		return r.URL.Query().Get(key)
	}
	selected := selection{Scope: value("scope"), Key: value("key"), Person: value("person"), Reference: value("reference")}
	records, err := c.sources.List(r.Context(), s.User.ID, "")
	if err != nil {
		return selected, err
	}
	switch selected.Scope {
	case "message":
		ref, e := decodeSource(selected.Reference)
		if e != nil {
			return selected, e
		}
		resolved, e := c.sources.Resolve(r.Context(), s.User.ID, ref)
		if e != nil {
			return selected, e
		}
		records = []timeline.Record{resolved.Record}
	case "thread":
		records, err = timeline.Related(records, selected.Key)
		if err != nil {
			return selected, err
		}
	case "person":
		address, e := netmail.ParseAddress(selected.Person)
		if e != nil || len(selected.Person) > 254 {
			return selected, timeline.ErrInput
		}
		selected.Person = address.Address
		filtered := []timeline.Record{}
		for _, record := range records {
			match := strings.EqualFold(record.Preview.Sender, address.Address)
			for _, to := range record.Preview.To {
				match = match || strings.EqualFold(to, address.Address)
			}
			if match {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	default:
		return selected, timeline.ErrInput
	}
	archived, err := c.collections.Archived(r.Context(), s.User.ID)
	if err != nil {
		return selected, err
	}
	seen := map[string]bool{}
	for _, record := range records {
		if seen[record.Identity.Key] || archive && archived[record.Identity.Key] {
			continue
		}
		seen[record.Identity.Key] = true
		selected.Total++
		if len(selected.Refs) < 500 {
			selected.Refs = append(selected.Refs, record.Ref)
		}
	}
	raw, _ := json.Marshal(selected.Refs)
	hash := sha256.Sum256(raw)
	selected.Hash = hex.EncodeToString(hash[:])
	return selected, nil
}
func (c *timelineController) bulkRoutes(mux *http.ServeMux, protected protectedRoute, mutation func(http.ResponseWriter, *http.Request, identity.Session) bool) {
	render := func(labels bool) func(http.ResponseWriter, *http.Request, identity.Session) {
		return func(w http.ResponseWriter, r *http.Request, s identity.Session) {
			selection, err := c.selectScope(r, s, !labels)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			items, err := c.collections.List(r.Context(), s.User.ID, "label")
			if err != nil {
				http.Error(w, "Labels unavailable", 503)
				return
			}
			webui.RenderBulk(w, c.config.Dev, webui.BulkData{Labels: labels, Items: items, CSRF: s.CSRF, Scope: selection.Scope, Key: selection.Key, Person: selection.Person, Reference: selection.Reference, Hash: selection.Hash, Count: len(selection.Refs), Total: selection.Total})
		}
	}
	mux.HandleFunc("GET /archive/preview", protected(render(false)))
	mux.HandleFunc("GET /labels/assign", protected(render(true)))
	apply := func(labels bool) func(http.ResponseWriter, *http.Request, identity.Session) {
		return func(w http.ResponseWriter, r *http.Request, s identity.Session) {
			if !mutation(w, r, s) {
				return
			}
			selection, err := c.selectScope(r, s, !labels)
			if err != nil || selection.Hash != r.FormValue("selection_hash") {
				http.Error(w, "This selection changed. Reopen its preview before applying.", 409)
				return
			}
			if len(selection.Refs) == 0 {
				http.Redirect(w, r, "/?notice=no-changes", 303)
				return
			}
			if labels {
				action := r.FormValue("action")
				if action != "add" && action != "remove" {
					http.Error(w, "Choose add or remove", 400)
					return
				}
				_, err = c.collections.LabelsMany(r.Context(), s.User.ID, selection.Refs, r.Form["label_id"], action == "add")
			} else {
				if r.FormValue("confirm") != "archive" {
					http.Error(w, "Confirm the displayed cached selection", 400)
					return
				}
				_, err = c.collections.ArchiveMany(r.Context(), s.User.ID, selection.Refs)
			}
			if err != nil {
				http.Error(w, "No bulk change committed: source, ownership or capacity changed. Reopen the preview.", 409)
				return
			}
			notice := "bulk-archived"
			if labels {
				notice = "labels-updated"
			}
			http.Redirect(w, r, "/?"+url.Values{"notice": {notice}}.Encode(), 303)
		}
	}
	mux.HandleFunc("POST /archive/apply", protected(apply(false)))
	mux.HandleFunc("POST /labels/assign", protected(apply(true)))
	mux.HandleFunc("GET /labels", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		items, err := c.collections.List(r.Context(), s.User.ID, "label")
		if err != nil {
			http.Error(w, "Labels unavailable", 503)
			return
		}
		webui.RenderLabels(w, c.config.Dev, webui.BulkData{Items: items, CSRF: s.CSRF})
	}))
}
