// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/rules"
	"mailspace.local/mailspace/internal/timeline"
	"mailspace.local/mailspace/internal/webui"
	"net/http"
	"net/url"
	"strconv"
)

func ruleRoutes(mux *http.ServeMux, config AuthConfig, store *identity.Store, protected protectedRoute, mutation func(http.ResponseWriter, *http.Request, identity.Session) bool) {
	if config.Accounts == nil || config.Sync == nil || config.Outbox == nil {
		return
	}
	sources := timeline.New(store, config.Accounts, config.Sync, config.Outbox)
	engine := rules.New(store.Database(), sources)
	groups := collections.New(store.Database(), sources)
	config.Sync.SetAfterSync(func(ctx context.Context, owner, account string) { _, _ = engine.Process(ctx, owner, account) })
	render := func(w http.ResponseWriter, r *http.Request, s identity.Session, preview *rules.Preview) {
		list, err := engine.List(r.Context(), s.User.ID)
		if err != nil {
			http.Error(w, "Rules unavailable", 503)
			return
		}
		spaces, err := groups.List(r.Context(), s.User.ID, "space")
		if err != nil {
			http.Error(w, "Spaces unavailable", 503)
			return
		}
		data := webui.RulesData{CSRF: s.CSRF, Rules: list, Spaces: spaces, Preview: preview}
		switch r.URL.Query().Get("notice") {
		case "saved":
			data.Notice = "Rule saved disabled. Preview it before enabling."
		case "limited":
			data.Notice = "Rule enabled, but this application pass was limited or interrupted. Check manual choices/source changes and the documented capacities; no mail was deleted."
		case "enabled":
			data.Notice = "Rule enabled for eligible cached and future mail. Application is bounded and manual choices win."
		case "disabled":
			data.Notice = "Rule disabled; previous assignments remain."
		case "removed":
			data.Notice = "Rule removed; messages and previous assignments remain."
		}
		if id := r.URL.Query().Get("edit"); id != "" {
			item, err := engine.Get(r.Context(), s.User.ID, id)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			data.Edit = &item
		}
		if token := r.URL.Query().Get("reference"); token != "" {
			ref, err := decodeSource(token)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			resolved, err := sources.Resolve(r.Context(), s.User.ID, ref)
			if err != nil || resolved.Record.Preview.Encrypted || resolved.Record.Own {
				http.NotFound(w, r)
				return
			}
			space, err := groups.Get(r.Context(), s.User.ID, "space", r.URL.Query().Get("space"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			data.Edit = &rules.Rule{Kind: "sender", Value: resolved.Record.Preview.Sender, SpaceID: space.ID}
		}
		if data.Edit == nil && (r.URL.Query().Get("from_filter") != "" || r.URL.Query().Get("query_filter") != "") {
			draft := rules.Rule{Kind: "subject", Value: r.URL.Query().Get("query_filter")}
			if sender := r.URL.Query().Get("from_filter"); sender != "" {
				draft.Kind = "sender"
				draft.Value = sender
			}
			if id := r.URL.Query().Get("space"); id != "" {
				if space, err := groups.Get(r.Context(), s.User.ID, "space", id); err == nil {
					draft.SpaceID = space.ID
				}
			}
			data.Edit = &draft
			data.Notice = "Rule draft: review the condition and destination before saving. It remains disabled until you approve it."
		}
		if webui.RenderRules(w, config.Dev, data) != nil {
			http.Error(w, "Unable to render rules", 500)
		}
	}
	mux.HandleFunc("GET /rules", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) { render(w, r, s, nil) }))
	save := protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		revision, _ := strconv.ParseInt(r.FormValue("revision"), 10, 64)
		if _, err := engine.Save(r.Context(), s.User.ID, r.PathValue("id"), r.FormValue("kind"), r.FormValue("value"), r.FormValue("space"), revision); err != nil {
			http.Error(w, "Check rule, Space ownership and current revision", 422)
			return
		}
		http.Redirect(w, r, "/rules?notice=saved", 303)
	})
	mux.HandleFunc("POST /rules", save)
	mux.HandleFunc("POST /rules/{id}", save)
	mux.HandleFunc("POST /rules/{id}/preview", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		preview, err := engine.Preview(r.Context(), s.User.ID, r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		render(w, r, s, &preview)
	}))
	mux.HandleFunc("POST /rules/{id}/enable", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		if value := r.FormValue("enabled"); value != "0" && value != "1" {
			http.Error(w, "Invalid rule state", 400)
			return
		}
		on := r.FormValue("enabled") == "1"
		if on && r.FormValue("consent") != "allow" {
			http.Error(w, "Explicit local automation approval required", 400)
			return
		}
		revision, _ := strconv.ParseInt(r.FormValue("revision"), 10, 64)
		if err := engine.Enable(r.Context(), s.User.ID, r.PathValue("id"), revision, on); err != nil {
			http.NotFound(w, r)
			return
		}
		notice := "disabled"
		if on {
			notice = "enabled"
			if _, err := engine.Process(r.Context(), s.User.ID, ""); err != nil {
				notice = "limited"
			}
		}
		http.Redirect(w, r, "/rules?"+url.Values{"notice": {notice}}.Encode(), 303)
	}))
	mux.HandleFunc("POST /rules/{id}/delete", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		revision, _ := strconv.ParseInt(r.FormValue("revision"), 10, 64)
		if err := engine.Delete(r.Context(), s.User.ID, r.PathValue("id"), revision); err != nil {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/rules?notice=removed", 303)
	}))
}
