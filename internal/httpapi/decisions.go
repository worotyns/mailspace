// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/decision"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/privacy"
	"mailspace.local/mailspace/internal/timeline"
	"mailspace.local/mailspace/internal/webui"
	"net/http"
	"net/url"
)

func decisionRoutes(mux *http.ServeMux, config AuthConfig, store *identity.Store, protected protectedRoute, mutation func(http.ResponseWriter, *http.Request, identity.Session) bool) {
	if config.Accounts == nil || config.Sync == nil || config.Outbox == nil {
		return
	}
	engine := config.Decisions
	if engine == nil {
		sources := timeline.New(store, config.Accounts, config.Sync, config.Outbox)
		engine = decision.New(store.Database(), sources, collections.New(store.Database(), sources), nil)
	}
	mux.HandleFunc("GET /automation", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		settings, err := engine.Settings(r.Context(), s.User.ID)
		if err != nil {
			http.Error(w, "Automation unavailable", 503)
			return
		}
		runs, err := engine.Runs(r.Context(), s.User.ID)
		if err != nil {
			http.Error(w, "Decision history unavailable", 503)
			return
		}
		items, _ := collections.New(store.Database(), nil).List(r.Context(), s.User.ID, "space")
		names := map[string]string{}
		for _, item := range items {
			names[item.ID] = item.Name
		}
		notice := ""
		switch r.URL.Query().Get("notice") {
		case "batch":
			notice = "Bounded check completed: up to 3 eligible unassigned messages checked. Preview leaves proposals unchanged; saved Auto mode can apply high-confidence results. Stored results may be reused."
		case "enable":
			notice = "Choose Manual previews only (no background requests), Review or Auto, then approve the displayed provider and payload before requesting a message preview."
		case "saved":
			notice = "Settings saved. Future decisions use this version; previous in-flight decisions cannot apply against it."
		case "ready":
			notice = "Decision recorded. Review the trace below."
		case "applied":
			notice = "Stored decision applied once; no new provider request."
		case "replayed":
			notice = "Stored decision passed current validation. Replay made no provider request or assignment."
		case "failed":
			notice = "Could not complete this action. Check provider configuration, source/Space revisions and manual overrides. No plaintext fallback or automatic retry."
		}
		if webui.RenderAutomation(w, config.Dev, webui.AutomationData{Email: s.User.Email, CSRF: s.CSRF, Available: engine.Available(), Provider: engine.ProviderName(), Settings: settings, Runs: runs, SpaceNames: names, Notice: notice}) != nil {
			http.Error(w, "Unable to render automation", 500)
		}
	}))
	mux.HandleFunc("POST /api/automation/client-selection", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "JSON required", 415)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32768)
		var input struct {
			Reference string                              `json:"reference"`
			Excerpt   string                              `json:"excerpt"`
			RequestID string                              `json:"requestId"`
			Consent   privacy.ClientClassificationConsent `json:"consent"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "Invalid explicit selection request", 400)
			return
		}
		ref, err := decodeSource(input.Reference)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		run, err := engine.EvaluateSelection(r.Context(), s.User.ID, ref, input.Excerpt, input.RequestID, input.Consent)
		if err != nil {
			http.Error(w, "Selection classification unavailable; check consent, manual/review mode and source revisions", 422)
			return
		}
		items, _ := collections.New(store.Database(), nil).List(r.Context(), s.User.ID, "space")
		labels := []map[string]string{}
		for _, id := range run.Proposal.SpaceIDs {
			name := "Unavailable Space"
			for _, item := range items {
				if item.ID == id {
					name = item.Name
				}
			}
			labels = append(labels, map[string]string{"id": id, "name": name})
		}
		reply(w, map[string]any{"id": run.ID, "spaces": labels, "confidence": run.Proposal.Confidence})
	}))
	mux.HandleFunc("POST /automation", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		if r.FormValue("mode") != "off" && r.FormValue("consent") != "allow" {
			http.Error(w, "Explicit provider payload consent required", 400)
			return
		}
		if r.FormValue("mode") != "off" && r.FormValue("provider") != engine.ProviderName() {
			http.Error(w, "Provider changed; reload and approve the displayed destination", 400)
			return
		}
		if engine.Save(r.Context(), s.User.ID, r.FormValue("mode"), r.FormValue("payload")) != nil {
			http.Error(w, "Check provider availability and classification settings", 422)
			return
		}
		http.Redirect(w, r, "/automation?notice=saved", 303)
	}))
	mux.HandleFunc("POST /automation/recheck", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		if r.FormValue("provider") != engine.ProviderName() {
			http.Error(w, "Provider changed; reload the command", 400)
			return
		}
		var err error
		switch r.FormValue("execution") {
		case "preview":
			_, err = engine.PreviewUnassigned(r.Context(), s.User.ID)
		case "saved":
			_, err = engine.CheckUnassigned(r.Context(), s.User.ID)
		default:
			http.Error(w, "Choose preview or saved mode explicitly", 400)
			return
		}
		notice := "batch"
		if errors.Is(err, decision.ErrDisabled) {
			notice = "enable"
		} else if err != nil {
			notice = "failed"
		}
		http.Redirect(w, r, "/automation?notice="+notice, 303)
	}))
	mux.HandleFunc("POST /automation/preview", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		ref, err := decodeSource(r.FormValue("reference"))
		var run decision.Run
		if err == nil {
			run, err = engine.Evaluate(r.Context(), s.User.ID, ref, true)
		}
		target := "/automation?notice=failed"
		if errors.Is(err, decision.ErrDisabled) {
			target = "/automation?notice=enable"
		}
		if err == nil {
			target = "/automation?notice=ready#run-" + run.ID
		}
		if r.Header.Get("HX-Request") == "true" {
			if err != nil {
				http.Error(w, "Unable to check suggestions. Open Space automation to review consent/provider settings. No automatic retry.", 422)
				return
			}
			sources := timeline.New(store, config.Accounts, config.Sync, config.Outbox)
			c := timelineController{config: config, store: store, sources: sources, collections: collections.New(store.Database(), sources)}
			c.renderMembership(w, r, s, ref)
			return
		}
		http.Redirect(w, r, target, 303)
	}))
	for _, action := range []string{"apply", "replay"} {
		mux.HandleFunc("POST /automation/runs/{id}/"+action, protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
			if !mutation(w, r, s) {
				return
			}
			var err error
			if action == "apply" {
				err = engine.Apply(r.Context(), s.User.ID, r.PathValue("id"), true)
			} else {
				err = engine.Replay(r.Context(), s.User.ID, r.PathValue("id"))
			}
			notice := "applied"
			if action == "replay" {
				notice = "replayed"
			}
			if err != nil {
				notice = "failed"
			}
			http.Redirect(w, r, "/automation?"+url.Values{"notice": {notice}}.Encode(), 303)
		}))
	}
}
