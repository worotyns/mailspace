// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/decision"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/privacy"
	"mailspace.local/mailspace/internal/timeline"
	"mailspace.local/mailspace/internal/webui"
	"net/http"
	"net/url"
	"strconv"
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
		case "bulk":
			applied, _ := strconv.Atoi(r.URL.Query().Get("applied"))
			skipped, _ := strconv.Atoi(r.URL.Query().Get("skipped"))
			if applied < 0 || applied > 50 || skipped < 0 || skipped > 50 {
				applied, skipped = 0, 0
			}
			notice = fmt.Sprintf("Applied %d stored decisions; skipped %d outdated or unavailable decisions. No new provider requests.", applied, skipped)
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
		case "busy-failed", "rate-failed", "changed-failed", "provider-failed", "input-failed", "timeout-failed", "provider-auth-failed", "provider-budget-failed", "provider-rate-failed":
			notice = decisionFailureMessage(r.URL.Query().Get("notice"))
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
			notice = decisionFailureNotice(err)
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
		target := "/automation?notice=" + decisionFailureNotice(err)
		if errors.Is(err, decision.ErrDisabled) {
			target = "/automation?notice=enable"
		}
		if err == nil {
			target = "/automation?notice=ready#run-" + run.ID
		}
		if r.Header.Get("HX-Request") == "true" {
			if err != nil {
				w.Header().Set("X-Mailspace-Decision-Notice", decisionFailureMessage(decisionFailureNotice(err)))
				http.Error(w, decisionFailureMessage(decisionFailureNotice(err)), 422)
				return
			}
			sources := timeline.New(store, config.Accounts, config.Sync, config.Outbox)
			c := timelineController{config: config, store: store, sources: sources, collections: collections.New(store.Database(), sources)}
			c.renderMembership(w, r, s, ref)
			return
		}
		http.Redirect(w, r, target, 303)
	}))

	mux.HandleFunc("POST /automation/bulk/apply", protected(func(w http.ResponseWriter, r *http.Request, session identity.Session) {
		if !mutation(w, r, session) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32768)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid bounded selection", 400)
			return
		}
		ids := r.Form["run_id"]
		if r.FormValue("scope") == "visible" {
			ids = r.Form["visible_id"]
		} else if r.FormValue("scope") != "selected" {
			http.Error(w, "Choose selected or visible decisions", 400)
			return
		}
		if len(ids) == 0 || len(ids) > 50 {
			http.Error(w, "Select between 1 and 50 decisions", 400)
			return
		}
		// Authorize the complete selection before any assignment; each Apply also
		// rechecks the current source/settings/Space/manual revisions transactionally.
		selected := []decision.Run{}
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			run, err := engine.Get(r.Context(), session.User.ID, id)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			selected = append(selected, run)
		}
		applied, skipped := 0, 0
		for _, run := range selected {
			if run.State != "review" && run.State != "dry-run" {
				skipped++
				continue
			}
			if err := engine.Apply(r.Context(), session.User.ID, run.ID, true); err != nil {
				skipped++
			} else {
				applied++
			}
		}
		http.Redirect(w, r, "/automation?"+url.Values{"notice": {"bulk"}, "applied": {strconv.Itoa(applied)}, "skipped": {strconv.Itoa(skipped)}}.Encode(), 303)
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
				notice = decisionFailureNotice(err)
			}
			http.Redirect(w, r, "/automation?"+url.Values{"notice": {notice}}.Encode(), 303)
		}))
	}
}

// Only fixed categories reach the UI; provider response bodies may contain mail.
func decisionFailureNotice(err error) string {
	var status *decision.ProviderHTTPError
	if errors.As(err, &status) {
		switch status.Status {
		case 401, 403:
			return "provider-auth-failed"
		case 402:
			return "provider-budget-failed"
		case 429:
			return "provider-rate-failed"
		}
	}
	switch {
	case errors.Is(err, decision.ErrBusy):
		return "busy-failed"
	case errors.Is(err, decision.ErrRateLimited):
		return "rate-failed"
	case errors.Is(err, decision.ErrChanged):
		return "changed-failed"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout-failed"
	case errors.Is(err, decision.ErrProvider):
		return "provider-failed"
	case errors.Is(err, decision.ErrInput):
		return "input-failed"
	default:
		return "failed"
	}
}
func decisionFailureMessage(kind string) string {
	switch kind {
	case "provider-auth-failed":
		return "The provider rejected authentication/access (HTTP 401/403). Check token permissions and model access. No automatic retry."
	case "provider-budget-failed":
		return "The provider rejected the request for budget/credit reasons (HTTP 402). Check balance and spending limits. No automatic retry."
	case "provider-rate-failed":
		return "The provider rate limit was reached (HTTP 429). Wait before explicitly trying again. No automatic retry."
	case "busy-failed":
		return "Another Space check is running. Wait for it to finish, then try again. No automatic retry."
	case "rate-failed":
		return "The limit of 6 new classification attempts per minute was reached. Wait one minute before checking again."
	case "changed-failed":
		return "This decision is outdated: the message, Spaces, settings or manual assignment changed. Request a fresh preview and review it."
	case "timeout-failed":
		return "The Space check timed out. No automatic retry was made."
	case "provider-failed":
		return "The provider request failed or returned an unsupported response. Check provider availability, model and credentials. No automatic retry."
	case "input-failed":
		return "This message or decision is not eligible for this action. Check its source and Space definitions."
	default:
		return "Could not complete this action. No plaintext fallback or automatic retry."
	}
}
