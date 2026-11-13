// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/outbox"
	"mailspace.local/mailspace/internal/timeline"
	"mailspace.local/mailspace/internal/webui"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type timelineController struct {
	config      AuthConfig
	store       *identity.Store
	sources     *timeline.Service
	collections *collections.Service
}

func decodeSource(value string) (mail.SourceRef, error) {
	var ref mail.SourceRef
	if len(value) > 4096 {
		return ref, timeline.ErrInput
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return ref, timeline.ErrInput
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&ref) != nil || decoder.Decode(new(any)) != io.EOF {
		return ref, timeline.ErrInput
	}
	return ref, nil
}
func timelineReturn(value string) string {
	if len(value) > 4096 {
		return "/"
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host != "" || parsed.Scheme != "" || (parsed.Path != "/" && parsed.Path != "/timeline") {
		return "/"
	}
	q := url.Values{}
	source := parsed.Query()
	for _, key := range []string{"space", "label", "person", "filter", "q", "day", "account", "reply", "compose", "thread"} {
		if value := source.Get(key); value != "" && len(value) <= 1000 {
			q.Set(key, value)
		}
	}
	return "/?" + q.Encode()
}
func (c *timelineController) data(r *http.Request, s identity.Session) (webui.TimelineData, error) {
	q := r.URL.Query()
	data := webui.TimelineData{Email: s.User.Email, UserID: s.User.ID, CSRF: s.CSRF, SessionExpires: s.ExpiresAt.UnixMilli(), Synthetic: c.config.Synthetic, AccountID: q.Get("account"), SpaceID: q.Get("space"), LabelID: q.Get("label"), Person: q.Get("person"), Filter: q.Get("filter"), Query: strings.TrimSpace(q.Get("q")), Day: q.Get("day"), Timezone: s.User.Timezone, ComposeNew: q.Get("compose") == "new", ThreadKey: q.Get("thread"), Nonce: rand.Text(), Title: "Your timeline", Description: "Messages, people and context. One place to keep things moving."}
	// The default queue contains only incoming mail still needing a Space.
	// All activity is an explicit view, never a second physical IMAP Inbox.
	if data.Filter == "" && data.SpaceID == "" && data.LabelID == "" && data.Person == "" && data.Query == "" && data.Day == "" && data.ThreadKey == "" && q.Get("reply") == "" && !data.ComposeNew {
		data.Filter = "unsorted"
	}
	data.Classifier = c.config.Decisions != nil && c.config.Decisions.Available()
	if len(data.Query) > 200 || len(data.Person) > 254 || (data.Filter != "" && data.Filter != "all" && data.Filter != "unsorted" && data.Filter != "unread") {
		return data, timeline.ErrInput
	}
	if data.ThreadKey != "" {
		if _, err := hex.DecodeString(data.ThreadKey); err != nil || len(data.ThreadKey) != 64 {
			return data, timeline.ErrInput
		}
	}
	if data.Day != "" {
		if _, err := time.Parse("2006-01-02", data.Day); err != nil {
			return data, timeline.ErrInput
		}
	}
	spaces, err := c.collections.List(r.Context(), s.User.ID, "space")
	if err != nil {
		return data, err
	}
	labels, err := c.collections.List(r.Context(), s.User.ID, "label")
	if err != nil {
		return data, err
	}
	if data.SpaceID != "" {
		item, err := c.collections.Get(r.Context(), s.User.ID, "space", data.SpaceID)
		if err != nil {
			return data, err
		}
		data.Title = item.Name
		data.Description = item.Description
	}
	if data.LabelID != "" {
		if label, err := c.collections.Get(r.Context(), s.User.ID, "label", data.LabelID); err != nil {
			return data, err
		} else {
			data.Title = "Label: " + label.Name
			data.Description = "Messages with this label."
		}
	}
	if data.Filter == "unsorted" {
		data.Title = "Unsorted"
		data.Description = "Give each message a home. Add it to one or several Spaces."
	}
	if data.Filter == "unread" {
		data.Title = "Unread activity"
	}
	records, err := c.sources.List(r.Context(), s.User.ID, data.AccountID)
	if err != nil {
		return data, err
	}
	memberships, err := c.collections.Memberships(r.Context(), s.User.ID)
	if err != nil {
		return data, err
	}
	accounts, err := c.store.Accounts(r.Context(), s.User.ID)
	if err != nil {
		return data, err
	}
	for _, account := range accounts {
		configured, e := c.config.Accounts.Get(r.Context(), s.User.ID, account.ID)
		if e != nil {
			return data, e
		}
		data.Accounts = append(data.Accounts, webui.AccountSummary{ID: account.ID, Name: account.Name, Address: configured.Address})
	}
	workspace, err := c.store.ActiveWorkspace(r.Context(), s.User.ID)
	if err != nil {
		return data, err
	}
	data.WorkspaceName = workspace.Name
	webui.FormatTimeline(records, spaces, labels, memberships, &data)
	if data.ThreadKey != "" {
		related, e := timeline.Related(records, data.ThreadKey)
		if e != nil {
			return data, e
		}
		if len(related) > 100 {
			position := 0
			for i := range related {
				if related[i].Identity.Key == data.ThreadKey {
					position = i
					break
				}
			}
			start := position - 50
			if start < 0 {
				start = 0
			}
			if start+100 > len(related) {
				start = len(related) - 100
			}
			related = related[start : start+100]
		}
		for i := range related {
			resolved, e := c.sources.Resolve(r.Context(), s.User.ID, related[i].Ref)
			if e != nil {
				return data, e
			}
			related[i] = resolved.Record
		}
		threadData := webui.TimelineData{Classifier: data.Classifier, Timezone: data.Timezone, CSRF: data.CSRF, ThreadKey: data.ThreadKey}
		webui.FormatTimeline(related, spaces, labels, memberships, &threadData)
		data.Messages = threadData.Messages
		data.Total = len(data.Messages)
		data.Title = "Email thread"
		if len(data.Messages) > 0 {
			data.Title = data.Messages[0].TopicLabel()
		}
		data.Description = "Original email thread · up to 100 recent cached messages. Missing history is not downloaded by opening this view."
		for i := range data.Messages {
			data.Messages[i].ReturnURL = data.CurrentURL()
			data.Messages[i].Selected = data.Messages[i].Identity.Key == data.ThreadKey
		}
	}

	if key := q.Get("reply"); key != "" {
		for i := range data.Messages {
			data.Messages[i].Selected = false
		}
		found := false
		for i := range data.Messages {
			if data.Messages[i].Identity.Key == key {
				resolved, err := c.sources.Resolve(r.Context(), s.User.ID, data.Messages[i].Ref)
				if err != nil {
					return data, err
				}
				copy := data.Messages[i]
				copy.Record = resolved.Record
				copy.Selected = true
				data.Reply = &copy
				data.Messages[i].Selected = true
				found = true
				break
			}
		}
		if !found {
			return data, timeline.ErrNotFound
		}
	}
	switch q.Get("notice") {
	case "synced":
		data.Notice = "Mail refreshed."
	case "sync-failed":
		data.Notice = "Mail refresh failed. Cached messages remain available; check account settings."
	case "account-connected":
		data.Notice = "Mailbox connected. Recent messages will appear as synchronization completes."
	case "saved":
		data.Notice = "Space saved."
	case "label-saved":
		data.Notice = "Label saved."
	case "assigned":
		data.Notice = "Message assignment updated."
	case "sync-requested":
		data.Notice = "Refresh requested. Recent mail will appear as synchronization completes."
	case "removed":
		data.Notice = "Collection removed; original mail is unchanged."
	}
	return data, nil
}
func (c *timelineController) render(w http.ResponseWriter, r *http.Request, s identity.Session) {
	data, err := c.data(r, s)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) || errors.Is(err, timeline.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Unable to open this timeline; check its filters and source availability", 400)
		}
		return
	}
	hash, err := webui.PreviewStyleHash(c.config.Dev)
	if err != nil {
		http.Error(w, "UI assets unavailable", 503)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' "+hash+"; img-src 'none'; connect-src 'self'; frame-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if err = webui.RenderTimeline(w, r, c.config.Dev, data); err != nil {
		http.Error(w, "Unable to render timeline", 500)
	}
}
func timelineRoutes(mux *http.ServeMux, config AuthConfig, store *identity.Store, protected protectedRoute, mutation func(http.ResponseWriter, *http.Request, identity.Session) bool, budget func(http.ResponseWriter, *http.Request, string) bool, release func()) bool {
	if config.Sync == nil || config.Accounts == nil || config.Outbox == nil {
		return false
	}
	source := timeline.New(store, config.Accounts, config.Sync, config.Outbox)
	c := &timelineController{config: config, store: store, sources: source, collections: collections.New(store.Database(), source)}
	mux.HandleFunc("GET /{$}", protected(c.render))
	mux.HandleFunc("GET /timeline", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		destination := timelineReturn("/?" + r.URL.RawQuery)
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", destination)
		}
		http.Redirect(w, r, destination, http.StatusSeeOther)
	}))
	mux.HandleFunc("GET /messages/collections", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		ref, err := decodeSource(r.URL.Query().Get("reference"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		c.renderMembership(w, r, s, ref)
	}))
	mux.HandleFunc("GET /api/messages/raw", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		ref, err := decodeSource(r.URL.Query().Get("reference"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		resolved, err := source.Resolve(r.Context(), s.User.ID, ref)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="message.eml"`)
		_, _ = w.Write(resolved.Raw)
	}))
	mux.HandleFunc("GET /messages/read", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		ref, err := decodeSource(r.URL.Query().Get("reference"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		resolved, err := source.Resolve(r.Context(), s.User.ID, ref)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		classifyReference, classifyProvider := "", ""
		if config.Decisions != nil && config.Decisions.Available() && resolved.Record.Preview.Encrypted {
			classifyReference = webui.EncodeSource(ref)
			classifyProvider = config.Decisions.ProviderName()
		}
		renderSecurePage(w, config, webui.AuthPageData{UserID: s.User.ID, SessionExpires: s.ExpiresAt.UnixMilli(), Email: s.User.Email, ClassifyReference: classifyReference, ClassifyProvider: classifyProvider, CSRF: s.CSRF, SecureRead: true, OrdinaryRead: !resolved.Record.Preview.Encrypted, RawURL: "/api/messages/raw?" + url.Values{"reference": {webui.EncodeSource(ref)}}.Encode(), ClaimedSender: resolved.Record.Preview.Sender, AccountID: ref.AccountID})
	}))
	mux.HandleFunc("POST /timeline/sync", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		list, err := store.Accounts(r.Context(), s.User.ID)
		if err != nil {
			http.Error(w, "Accounts unavailable", 503)
			return
		}
		for _, account := range list {
			config.Sync.Schedule(s.User.ID, account.ID)
		}
		target := timelineReturn(r.FormValue("return_to"))
		parsed, _ := url.Parse(target)
		q := parsed.Query()
		q.Set("notice", "sync-requested")
		parsed.RawQuery = q.Encode()
		http.Redirect(w, r, parsed.String(), 303)
	}))
	for _, kind := range []string{"space", "label"} {
		prefix := "/spaces"
		if kind == "label" {
			prefix = "/labels"
		}
		mux.HandleFunc("GET "+prefix+"/new", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
			webui.RenderCollectionForm(w, config.Dev, s.User.Email, s.CSRF, collections.Item{Kind: kind, Color: "lilac"}, "")
		}))
		mux.HandleFunc("GET "+prefix+"/{id}/edit", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
			item, err := c.collections.Get(r.Context(), s.User.ID, kind, r.PathValue("id"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			webui.RenderCollectionForm(w, config.Dev, s.User.Email, s.CSRF, item, "")
		}))
		mux.HandleFunc("POST "+prefix, protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
			if !mutation(w, r, s) {
				return
			}
			item, err := c.collections.Create(r.Context(), s.User.ID, kind, r.FormValue("name"), r.FormValue("description"), r.FormValue("color"))
			if err != nil {
				w.WriteHeader(422)
				webui.RenderCollectionForm(w, config.Dev, s.User.Email, s.CSRF, collections.Item{Kind: kind, Name: r.FormValue("name"), Description: r.FormValue("description"), Color: r.FormValue("color")}, "Unable to save. Check the name, description and collection limit.")
				return
			}
			key := "space"
			if kind == "label" {
				key = "label"
			}
			http.Redirect(w, r, "/?"+url.Values{key: {item.ID}, "notice": {"saved"}}.Encode(), 303)
		}))
		mux.HandleFunc("POST "+prefix+"/{id}/delete", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
			if !mutation(w, r, s) {
				return
			}
			if r.FormValue("confirm") != "remove" {
				http.Error(w, "Confirm removal explicitly", 400)
				return
			}
			if err := c.collections.Remove(r.Context(), s.User.ID, kind, r.PathValue("id")); err != nil {
				http.NotFound(w, r)
				return
			}
			http.Redirect(w, r, "/?notice=removed", 303)
		}))
		mux.HandleFunc("POST "+prefix+"/{id}", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
			if !mutation(w, r, s) {
				return
			}
			revision, _ := strconv.ParseInt(r.FormValue("revision"), 10, 64)
			item, err := c.collections.Update(r.Context(), s.User.ID, kind, r.PathValue("id"), r.FormValue("name"), r.FormValue("description"), r.FormValue("color"), revision)
			if err != nil {
				http.Error(w, "Unable to update this definition; reload it before retrying", 422)
				return
			}
			key := "space"
			if kind == "label" {
				key = "label"
			}
			http.Redirect(w, r, "/?"+url.Values{key: {item.ID}, "notice": {"saved"}}.Encode(), 303)
		}))
	}
	mux.HandleFunc("POST /collections/{id}/membership", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		ref, err := decodeSource(r.FormValue("reference"))
		if err != nil {
			http.Error(w, "Invalid message reference", 400)
			return
		}
		kind := r.FormValue("kind")
		selected := r.FormValue("selected")
		if selected != "0" && selected != "1" {
			http.Error(w, "Explicit membership selection required", 400)
			return
		}
		if err = c.collections.Assign(r.Context(), s.User.ID, kind, r.PathValue("id"), ref, r.FormValue("selected") == "1"); err != nil {
			http.Error(w, "Unable to update assignment; the source or Space may have changed", 404)
			return
		}
		if r.Header.Get("HX-Request") == "true" {
			c.renderMembership(w, r, s, ref)
			return
		}
		target := timelineReturn(r.FormValue("return_to"))
		http.Redirect(w, r, target, 303)
	}))
	mux.HandleFunc("POST /timeline/replies/plain", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
		if !mutation(w, r, s) {
			return
		}
		if !budget(w, r, s.User.ID) {
			return
		}
		defer release()
		spaceID := r.FormValue("space")
		if spaceID != "" {
			if _, err := c.collections.Get(r.Context(), s.User.ID, "space", spaceID); err != nil {
				http.NotFound(w, r)
				return
			}
		}
		draft := outbox.Draft{To: r.FormValue("to"), CC: r.FormValue("cc"), BCC: r.FormValue("bcc"), Subject: r.FormValue("subject"), Body: r.FormValue("body"), AllowPlaintext: r.FormValue("allow_plaintext") == "1"}
		nonce := r.FormValue("request_id")
		var record outbox.Record
		var err error
		accountID := r.FormValue("account")
		if token := r.FormValue("reference"); token != "" {
			ref, e := decodeSource(token)
			if e != nil {
				http.Error(w, "Invalid reply target", 400)
				return
			}
			resolved, e := source.Resolve(r.Context(), s.User.ID, ref)
			if e != nil {
				http.NotFound(w, r)
				return
			}
			target := webui.TimelineMessage{Record: resolved.Record}
			draft.Subject = target.Subject()
			if draft.To == "" {
				draft.To = target.Recipient()
			}
			accountID = ref.AccountID
			record, err = config.Outbox.QueueReply(r.Context(), s.User.ID, nonce, ref, draft)
		} else {
			record, err = config.Outbox.Queue(r.Context(), s.User.ID, accountID, nonce, draft)
		}
		if err != nil {
			w.WriteHeader(422)
			message := "Could not prepare this reply. Check the recipient and size; open Outbox before reusing a previous send."
			if errors.Is(err, outbox.ErrEncryption) {
				message = "Choose Send unencrypted explicitly. Encrypted originals must be answered with encryption."
			}
			webui.RenderReplyResult(w, config.Dev, webui.ReplyResult{Error: message})
			return
		}
		_ = config.Outbox.Dispatch(r.Context(), s.User.ID, record.ID)
		if updated, e := config.Outbox.Get(r.Context(), s.User.ID, record.ID); e == nil {
			record = updated
		}
		assignmentWarning := ""
		if spaceID != "" {
			account, e := config.Accounts.Get(r.Context(), s.User.ID, accountID)
			if e == nil {
				e = c.collections.Assign(r.Context(), s.User.ID, "space", spaceID, mail.SourceRef{Kind: "outbox", AccountID: accountID, Revision: account.Revision, OutgoingID: record.ID}, true)
			}
			if e != nil {
				assignmentWarning = "The send attempt is recorded, but its Space assignment changed. Check Outbox."
			}
		}
		if r.Header.Get("HX-Request") != "true" {
			http.Redirect(w, r, timelineReturn(r.FormValue("return_to")), 303)
			return
		}
		webui.RenderReplyResult(w, config.Dev, webui.ReplyResult{Record: record, Warning: assignmentWarning, NextURL: timelineReturn(r.FormValue("return_to")), OutboxURL: "/outbox?" + url.Values{"account": {record.AccountID}, "message": {record.ID}}.Encode()})
	}))
	return true
}
func (c *timelineController) renderMembership(w http.ResponseWriter, r *http.Request, s identity.Session, ref mail.SourceRef) {
	resolved, err := c.sources.Resolve(r.Context(), s.User.ID, ref)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	spaces, err := c.collections.List(r.Context(), s.User.ID, "space")
	if err != nil {
		http.Error(w, "Collections unavailable", 503)
		return
	}
	labels, err := c.collections.List(r.Context(), s.User.ID, "label")
	if err != nil {
		http.Error(w, "Collections unavailable", 503)
		return
	}
	members, err := c.collections.Memberships(r.Context(), s.User.ID)
	if err != nil {
		http.Error(w, "Assignments unavailable", 503)
		return
	}
	message := webui.TimelineMessage{Classifier: c.config.Decisions != nil && c.config.Decisions.Available() && !resolved.Record.Own && !resolved.Record.Preview.Encrypted && resolved.Record.Preview.Status == "ready", Record: resolved.Record, Token: webui.EncodeSource(ref), CSRF: s.CSRF, ReturnURL: timelineReturn(r.FormValue("return_to")), Expanded: true}
	for _, item := range spaces {
		message.Spaces = append(message.Spaces, webui.CollectionView{Item: item})
	}
	for _, item := range labels {
		message.Labels = append(message.Labels, webui.CollectionView{Item: item})
	}
	for _, id := range members[resolved.Record.Identity.Key] {
		for _, item := range append(spaces, labels...) {
			if item.ID == id {
				message.Memberships = append(message.Memberships, webui.CollectionView{Item: item})
			}
		}
	}
	if r.Header.Get("HX-Request") == "true" {
		webui.RenderMembership(w, c.config.Dev, message)
	} else {
		webui.RenderMembershipPage(w, c.config.Dev, message)
	}
}
