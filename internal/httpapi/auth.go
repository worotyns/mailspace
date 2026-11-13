// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mailnet"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"mailspace.local/mailspace/internal/webui"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AuthConfig struct {
	Synthetic            bool
	Outbox               *outbox.Service
	Sync                 *mailsync.Service
	Origin               string
	InsecureLoopback     bool
	Dev                  bool
	Accounts             *accounts.Service
	MailEndpointsEnabled bool
}
type windowEntry struct {
	started  time.Time
	attempts int
}
type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]windowEntry
}

func (l *loginLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, exists := l.entries[key]
	if !exists && len(l.entries) >= 4096 {
		for k, v := range l.entries {
			if now.Sub(v.started) >= time.Minute {
				delete(l.entries, k)
			}
		}
		if len(l.entries) >= 4096 {
			return false
		}
	}
	if now.Sub(entry.started) >= time.Minute || entry.started.IsZero() {
		entry = windowEntry{started: now}
	}
	if entry.attempts >= 10 {
		return false
	}
	entry.attempts++
	l.entries[key] = entry
	return true
}
func NewPersonal(sourceDir string, store *identity.Store, config AuthConfig) (http.Handler, error) {
	origin, err := url.Parse(config.Origin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return nil, errors.New("MAILSPACE_ORIGIN must be an absolute origin without credentials, query or path")
	}
	secure := origin.Scheme == "https"
	loopback := origin.Hostname() == "localhost"
	if ip := net.ParseIP(origin.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if !secure && !(config.InsecureLoopback && origin.Scheme == "http" && loopback) {
		return nil, errors.New("personal mode requires HTTPS; explicit insecure HTTP is allowed only for loopback development")
	}
	expectedOrigin := origin.Scheme + "://" + origin.Host
	cookieName := "__Host-mailspace"
	if !secure {
		cookieName = "mailspace_dev_session"
	}
	limiter := &loginLimiter{entries: map[string]windowEntry{}}
	slots := make(chan struct{}, 2)
	mux := http.NewServeMux()
	render := func(w http.ResponseWriter, name string, data webui.AuthPageData) {
		if err := webui.RenderAuth(w, config.Dev, name, data); err != nil {
			http.Error(w, "Unable to render account view", 500)
		}
	}
	sessionFor := func(r *http.Request) (identity.Session, error) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			return identity.Session{}, identity.ErrNotFound
		}
		return store.Session(r.Context(), cookie.Value)
	}
	protected := func(next func(http.ResponseWriter, *http.Request, identity.Session)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			session, err := sessionFor(r)
			if err != nil {
				if errors.Is(err, identity.ErrNotFound) {
					if r.Header.Get("HX-Request") == "true" {
						w.Header().Set("HX-Redirect", "/login")
						http.Error(w, "Authentication required", 401)
					} else if r.Method == "GET" && !strings.HasPrefix(r.URL.Path, "/api/") {
						http.Redirect(w, r, "/login", 303)
					} else {
						http.Error(w, "Authentication required", 401)
					}
				} else {
					http.Error(w, "Session service unavailable", 503)
				}
				return
			}
			next(w, r, session)
		}
	}
	clearCookie := func(w http.ResponseWriter) {
		http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	}
	mutation := func(w http.ResponseWriter, r *http.Request, session identity.Session) bool {
		if r.Header.Get("Origin") != expectedOrigin {
			http.Error(w, "Origin not allowed", 403)
			return false
		}
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			limit := int64(32768)
			if r.URL.Path == "/outbox/send" || r.URL.Path == "/timeline/replies/plain" {
				limit = 4 << 20
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Invalid form", 400)
				return false
			}
			token = r.PostForm.Get("csrf")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(session.CSRF)) != 1 {
			http.Error(w, "CSRF token required", 403)
			return false
		}
		return true
	}

	connectionLimiter := &loginLimiter{entries: map[string]windowEntry{}}
	connectionSlots := make(chan struct{}, 4)
	withConnectionBudget := func(w http.ResponseWriter, r *http.Request, userID string) bool {
		if !connectionLimiter.allow(userID, time.Now()) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too many connection checks", 429)
			return false
		}
		select {
		case connectionSlots <- struct{}{}:
			return true
		default:
			http.Error(w, "Connection checks busy; try again", 503)
			return false
		}
	}
	formFor := func(value accounts.Config) webui.AccountForm {
		return webui.AccountForm{SentMailbox: value.SentMailbox, ID: value.ID, Name: value.Name, Address: value.Address, IMAPHost: value.IMAP.Host, IMAPPort: value.IMAP.Port, IMAPTLS: value.IMAP.TLS, IMAPUser: value.IMAPUsername, SMTPHost: value.SMTP.Host, SMTPPort: value.SMTP.Port, SMTPTLS: value.SMTP.TLS, SMTPUser: value.SMTPUsername}
	}
	mux.HandleFunc("GET /accounts/new", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if config.Accounts == nil {
			http.Error(w, "Mail account setup unavailable", 503)
			return
		}
		render(w, "account-settings", webui.AuthPageData{Email: s.User.Email, CSRF: s.CSRF, MailEndpointsEnabled: config.MailEndpointsEnabled, Form: webui.AccountForm{SentMailbox: "Sent", Address: s.User.Email, IMAPPort: 993, IMAPTLS: "tls", SMTPPort: 465, SMTPTLS: "tls", SameCredentials: true}})
	}))
	mux.HandleFunc("GET /accounts/{id}/edit", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if config.Accounts == nil {
			http.Error(w, "Mail account setup unavailable", 503)
			return
		}
		value, err := config.Accounts.Get(r.Context(), s.User.ID, r.PathValue("id"))
		if err != nil {
			if errors.Is(err, accounts.ErrNotFound) {
				http.NotFound(w, r)
			} else {
				http.Error(w, "Mail account unavailable", 503)
			}
			return
		}
		render(w, "account-settings", webui.AuthPageData{Email: s.User.Email, CSRF: s.CSRF, MailEndpointsEnabled: config.MailEndpointsEnabled, Form: formFor(value)})
	}))
	saveAccount := protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if config.Accounts == nil {
			http.Error(w, "Mail account setup unavailable", 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32768)
		if !mutation(w, r, s) {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		imapPort, _ := strconv.Atoi(r.PostForm.Get("imap_port"))
		smtpPort, _ := strconv.Atoi(r.PostForm.Get("smtp_port"))
		input := accounts.Input{SentMailbox: r.PostForm.Get("sent_mailbox"), Name: r.PostForm.Get("name"), Address: r.PostForm.Get("address"), IMAP: mailnet.Endpoint{Host: r.PostForm.Get("imap_host"), Port: imapPort, TLS: r.PostForm.Get("imap_tls")}, SMTP: mailnet.Endpoint{Host: r.PostForm.Get("smtp_host"), Port: smtpPort, TLS: r.PostForm.Get("smtp_tls")}, IMAPLogin: mailnet.Login{Username: r.PostForm.Get("imap_user"), Password: r.PostForm.Get("imap_password")}, SMTPLogin: mailnet.Login{Username: r.PostForm.Get("smtp_user"), Password: r.PostForm.Get("smtp_password")}}
		same := r.PostForm.Get("same_credentials") == "1"
		if same {
			input.SMTPLogin = input.IMAPLogin
		}
		if !withConnectionBudget(w, r, s.User.ID) {
			return
		}
		defer func() { <-connectionSlots }()
		saved, err := config.Accounts.Save(r.Context(), s.User.ID, r.PathValue("id"), input)
		if err != nil {
			if errors.Is(err, accounts.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			message := "Could not save the account. Check configuration and try again."
			status := 503
			var checkErr mailnet.CheckError
			if errors.Is(err, accounts.ErrInput) {
				message = "Check all server settings, email address and login fields."
				status = 400
			} else if errors.As(err, &checkErr) {
				message = checkErr.Error() + ". Check the server, TLS, credentials and administrator allowlist."
				status = 422
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(status)
			form := webui.AccountForm{SentMailbox: input.SentMailbox, ID: r.PathValue("id"), Name: input.Name, Address: input.Address, IMAPHost: input.IMAP.Host, IMAPPort: input.IMAP.Port, IMAPTLS: input.IMAP.TLS, IMAPUser: input.IMAPLogin.Username, SMTPHost: input.SMTP.Host, SMTPPort: input.SMTP.Port, SMTPTLS: input.SMTP.TLS, SMTPUser: input.SMTPLogin.Username, SameCredentials: same}
			render(w, "account-settings", webui.AuthPageData{Email: s.User.Email, CSRF: s.CSRF, Message: message, Form: form, MailEndpointsEnabled: config.MailEndpointsEnabled})
			return
		}
		if config.Sync != nil {
			config.Sync.Schedule(s.User.ID, saved.ID)
		}
		http.Redirect(w, r, "/?notice=account-connected", 303)
	})
	mux.HandleFunc("POST /accounts", saveAccount)
	mux.HandleFunc("POST /accounts/{id}", saveAccount)
	mux.HandleFunc("POST /accounts/{id}/check", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if config.Accounts == nil {
			http.Error(w, "Mail account setup unavailable", 503)
			return
		}
		if !mutation(w, r, s) {
			return
		}
		if !withConnectionBudget(w, r, s.User.ID) {
			return
		}
		defer func() { <-connectionSlots }()
		if err := config.Accounts.Check(r.Context(), s.User.ID, r.PathValue("id")); err != nil {
			if errors.Is(err, accounts.ErrNotFound) {
				http.NotFound(w, r)
			} else {
				http.Error(w, "Mail connection check failed; verify settings and administrator allowlist", 422)
			}
			return
		}
		http.Redirect(w, r, "/?notice=account-checked", 303)
	}))

	// Old bookmarks keep their account scope, but cannot revive retired views.
	mux.HandleFunc("GET /mail", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if config.Accounts == nil {
			http.Error(w, "Mail unavailable", 503)
			return
		}
		id := r.URL.Query().Get("account")
		if _, err := config.Accounts.Get(r.Context(), s.User.ID, id); err != nil {
			http.NotFound(w, r)
			return
		}
		destination := "/?" + url.Values{"account": {id}}.Encode()
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", destination)
		}
		http.Redirect(w, r, destination, http.StatusSeeOther)
	}))
	mux.HandleFunc("POST /accounts/{id}/sync", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		if config.Sync == nil {
			http.Error(w, "Sync unavailable", 503)
			return
		}
		if !withConnectionBudget(w, r, s.User.ID) {
			return
		}
		defer func() { <-connectionSlots }()
		id := r.PathValue("id")
		notice := "synced"
		if err := config.Sync.Sync(r.Context(), s.User.ID, id); err != nil {
			if errors.Is(err, accounts.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			notice = "sync-failed"
		}
		http.Redirect(w, r, "/?"+url.Values{"account": {id}, "notice": {notice}}.Encode(), 303)
	}))

	mux.HandleFunc("GET /compose", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if config.Outbox == nil || config.Accounts == nil {
			http.Error(w, "Compose unavailable", 503)
			return
		}
		account, err := config.Accounts.Get(r.Context(), s.User.ID, r.URL.Query().Get("account"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		form := webui.ComposeForm{AccountID: account.ID, AccountName: account.Name, Address: account.Address, Nonce: rand.Text()}
		if r.URL.Query().Get("uid") != "" {
			uid, e := strconv.ParseUint(r.URL.Query().Get("uid"), 10, 32)
			validity, e2 := strconv.ParseUint(r.URL.Query().Get("validity"), 10, 32)
			if e != nil || e2 != nil {
				http.Error(w, "Invalid reply", 400)
				return
			}
			parent, err := config.Outbox.Parent(r.Context(), s.User.ID, account.ID, r.URL.Query().Get("folder"), uint32(validity), uint32(uid))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			if parent.Encrypted {
				http.Error(w, "Encrypted replies require client-side OpenPGP. Plaintext downgrade is blocked.", 409)
				return
			}
			form.ReplyFolder = r.URL.Query().Get("folder")
			form.ReplyUID = uint32(uid)
			form.ReplyValidity = uint32(validity)
			form.To = parent.Sender
			if parent.ReplyTo != "" {
				form.To = parent.ReplyTo
			}
			if strings.EqualFold(parent.Sender, account.Address) {
				form.To = ""
				for _, to := range parent.To {
					if !strings.EqualFold(to, account.Address) {
						form.To = to
						break
					}
				}
			}
			form.Subject = parent.Subject
			if !strings.HasPrefix(strings.ToLower(form.Subject), "re:") {
				form.Subject = "Re: " + form.Subject
			}
		}
		render(w, "compose", webui.AuthPageData{Email: s.User.Email, CSRF: s.CSRF, Compose: form})
	}))
	mux.HandleFunc("POST /outbox/send", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if config.Outbox == nil {
			http.Error(w, "Sending unavailable", 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
		if !mutation(w, r, s) {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid compose form", 400)
			return
		}
		if !withConnectionBudget(w, r, s.User.ID) {
			return
		}
		defer func() { <-connectionSlots }()
		uid, _ := strconv.ParseUint(r.PostForm.Get("reply_uid"), 10, 32)
		validity, _ := strconv.ParseUint(r.PostForm.Get("reply_validity"), 10, 32)
		draft := outbox.Draft{To: r.PostForm.Get("to"), CC: r.PostForm.Get("cc"), BCC: r.PostForm.Get("bcc"), Subject: r.PostForm.Get("subject"), Body: r.PostForm.Get("body"), AllowPlaintext: r.PostForm.Get("allow_plaintext") == "1", ReplyFolder: r.PostForm.Get("reply_folder"), ReplyUID: uint32(uid), ReplyValidity: uint32(validity)}
		record, err := config.Outbox.Queue(r.Context(), s.User.ID, r.PostForm.Get("account"), r.PostForm.Get("request_id"), draft)
		if err != nil {
			if errors.Is(err, accounts.ErrNotFound) || errors.Is(err, outbox.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			message := "Unable to prepare this message. Check recipients, size and account settings."
			if errors.Is(err, outbox.ErrEncryption) {
				message = "Choose explicitly to send without OpenPGP encryption. Encrypted replies cannot be downgraded."
			}
			if errors.Is(err, outbox.ErrConflict) {
				message = "This send form was already used. Open Outbox to check the original request."
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(422)
			render(w, "compose", webui.AuthPageData{Email: s.User.Email, CSRF: s.CSRF, Message: message, Compose: webui.ComposeForm{AccountID: r.PostForm.Get("account"), Nonce: r.PostForm.Get("request_id"), To: draft.To, CC: draft.CC, BCC: draft.BCC, Subject: draft.Subject, Body: draft.Body, AllowPlaintext: draft.AllowPlaintext, ReplyFolder: draft.ReplyFolder, ReplyUID: draft.ReplyUID, ReplyValidity: draft.ReplyValidity}})
			return
		}
		_ = config.Outbox.Dispatch(r.Context(), s.User.ID, record.ID)
		http.Redirect(w, r, "/outbox?"+url.Values{"account": {record.AccountID}, "message": {record.ID}}.Encode(), 303)
	}))
	mux.HandleFunc("GET /outbox", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if config.Outbox == nil || config.Accounts == nil {
			http.Error(w, "Outbox unavailable", 503)
			return
		}
		account, err := config.Accounts.Get(r.Context(), s.User.ID, r.URL.Query().Get("account"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		records, err := config.Outbox.List(r.Context(), s.User.ID, account.ID)
		if err != nil {
			http.Error(w, "Outbox unavailable", 503)
			return
		}
		outgoing := []webui.OutgoingRecord{}
		for _, item := range records {
			outgoing = append(outgoing, webui.OutgoingRecord{ID: item.ID, Subject: item.Subject, State: item.State, CopyState: item.CopyState, CreatedAt: item.CreatedAt.Format("02 Jan 15:04")})
		}
		render(w, "outbox", webui.AuthPageData{Email: s.User.Email, CSRF: s.CSRF, AccountID: account.ID, AccountName: account.Name, Outgoing: outgoing, OutgoingID: r.URL.Query().Get("message")})
	}))

	mux.HandleFunc("GET /profile", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		render(w, "profile", webui.AuthPageData{Email: s.User.Email, CSRF: s.CSRF, ProfileName: s.User.Name, Timezone: s.User.Timezone})
	}))
	mux.HandleFunc("POST /profile", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		if err := store.UpdateProfile(r.Context(), s.User.ID, r.PostForm.Get("display_name"), r.PostForm.Get("timezone")); err != nil {
			http.Error(w, "Invalid profile name or IANA timezone", 422)
			return
		}
		http.Redirect(w, r, "/profile", 303)
	}))
	mux.HandleFunc("GET /workspaces", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		list, err := store.Workspaces(r.Context(), s.User.ID)
		if err != nil {
			http.Error(w, "Workspaces unavailable", 503)
			return
		}
		active, err := store.ActiveWorkspace(r.Context(), s.User.ID)
		if err != nil {
			http.Error(w, "Workspace unavailable", 503)
			return
		}
		items := []webui.WorkspaceSummary{}
		for _, item := range list {
			items = append(items, webui.WorkspaceSummary{ID: item.ID, Name: item.Name, Role: item.Role, Active: item.ID == active.ID})
		}
		render(w, "workspaces", webui.AuthPageData{Email: s.User.Email, CSRF: s.CSRF, Workspaces: items, Workspace: webui.WorkspaceSummary{ID: active.ID, Name: active.Name, Role: active.Role}})
	}))
	mux.HandleFunc("GET /workspaces/{id}", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		workspace, err := store.Workspace(r.Context(), s.User.ID, r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		members, err := store.Members(r.Context(), s.User.ID, workspace.ID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		items := []webui.MemberSummary{}
		for _, m := range members {
			items = append(items, webui.MemberSummary{UserID: m.UserID, Name: m.Name, Email: m.Email, Owner: m.Owner})
		}
		render(w, "workspace-members", webui.AuthPageData{Email: s.User.Email, CSRF: s.CSRF, Workspace: webui.WorkspaceSummary{ID: workspace.ID, Name: workspace.Name, Role: workspace.Role}, Members: items})
	}))
	mux.HandleFunc("POST /workspaces", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		workspace, err := store.CreateWorkspace(r.Context(), s.User.ID, r.PostForm.Get("name"))
		if err != nil {
			http.Error(w, "Unable to create workspace; check name and limit", 422)
			return
		}
		if err = store.SwitchWorkspace(r.Context(), s.User.ID, workspace.ID); err != nil {
			http.Error(w, "Workspace created; select it from the workspace list", 503)
			return
		}
		http.Redirect(w, r, "/workspaces/"+workspace.ID, 303)
	}))
	mux.HandleFunc("POST /workspaces/{id}/switch", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		id := r.PathValue("id")
		if id == "personal" {
			id = ""
		}
		if err := store.SwitchWorkspace(r.Context(), s.User.ID, id); err != nil {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/", 303)
	}))
	mux.HandleFunc("POST /workspaces/{id}/members", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		if err := store.AddMember(r.Context(), s.User.ID, r.PathValue("id"), r.PostForm.Get("email")); err != nil {
			http.Error(w, "Unable to add member. You must own the workspace and the member must already have a local account.", 422)
			return
		}
		http.Redirect(w, r, "/workspaces/"+r.PathValue("id"), 303)
	}))
	mux.HandleFunc("POST /workspaces/{id}/members/{member}/remove", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		if err := store.RemoveMember(r.Context(), s.User.ID, r.PathValue("id"), r.PathValue("member")); err != nil {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/workspaces/"+r.PathValue("id"), 303)
	}))
	mux.HandleFunc("POST /workspaces/{id}/rename", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		if err := store.RenameWorkspace(r.Context(), s.User.ID, r.PathValue("id"), r.PostForm.Get("name")); err != nil {
			http.Error(w, "Unable to rename workspace", 422)
			return
		}
		http.Redirect(w, r, "/workspaces/"+r.PathValue("id"), 303)
	}))

	hasTimeline := timelineRoutes(mux, config, store, protected, mutation, withConnectionBudget, func() { <-connectionSlots })
	cryptoRoutes(mux, config, store, protected, mutation, withConnectionBudget, func() { <-connectionSlots })
	mux.HandleFunc("GET /keys", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'none'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		render(w, "keys", webui.AuthPageData{Email: s.User.Email, UserID: s.User.ID, ProfileName: s.User.Name, SessionExpires: s.ExpiresAt.UnixMilli()})
	}))

	mux.Handle("GET /assets/", webui.Assets(config.Dev))
	mux.Handle("GET /source.tar.gz", http.FileServer(http.Dir(sourceDir)))
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"status": "ok", "mode": "personal"})
	})
	mux.HandleFunc("GET /api/capabilities", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"mode": "personal", "synthetic": config.Synthetic, "identity": true, "workspaces": true, "spaces": hasTimeline, "unifiedTimeline": hasTimeline, "localKeyVault": true, "encryptedMimeAlpha": config.Outbox != nil && config.Sync != nil, "threadSharing": false, "mailAccounts": config.Accounts != nil, "mail": config.Sync != nil, "readOnlyMail": config.Sync != nil, "sending": config.Outbox != nil, "openpgp": false, "telemetry": false, "modules": map[string]bool{"team": false, "ai": false, "drive": false, "decisionEngine": false, "fullText": false, "semanticSearch": false}})
	})
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		if _, err := sessionFor(r); err == nil {
			http.Redirect(w, r, "/", 303)
			return
		}
		render(w, "login", webui.AuthPageData{})
	})
	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != expectedOrigin {
			http.Error(w, "Origin not allowed", 403)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !limiter.allow(host, time.Now()) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too many sign-in attempts", 429)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Sign-in busy; try again", 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err = r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		user, err := store.Authenticate(r.Context(), r.PostForm.Get("email"), r.PostForm.Get("password"))
		if err != nil {
			if errors.Is(err, identity.ErrCredentials) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(401)
				render(w, "login", webui.AuthPageData{Message: "Email or password not recognized."})
			} else {
				http.Error(w, "Sign-in service unavailable", 503)
			}
			return
		}
		if old, err := r.Cookie(cookieName); err == nil {
			if err = store.RevokeSession(r.Context(), old.Value); err != nil {
				http.Error(w, "Session rotation failed", 503)
				return
			}
		}
		token, session, err := store.NewSession(r.Context(), user.ID)
		if err != nil {
			http.Error(w, "Unable to create session", 503)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", Secure: secure, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: session.ExpiresAt, MaxAge: int(identity.SessionTTL.Seconds())})
		http.Redirect(w, r, "/", 303)
	})
	mux.HandleFunc("GET /api/session", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		reply(w, map[string]any{"user": s.User, "csrfToken": s.CSRF, "expiresAt": s.ExpiresAt})
	}))
	mux.HandleFunc("GET /api/accounts", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		accounts, err := store.Accounts(r.Context(), s.User.ID)
		if err != nil {
			http.Error(w, "Accounts unavailable", 503)
			return
		}
		reply(w, accounts)
	}))
	mux.HandleFunc("GET /api/accounts/{id}", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		account, err := store.Account(r.Context(), s.User.ID, r.PathValue("id"))
		if err != nil {
			if errors.Is(err, identity.ErrNotFound) {
				http.NotFound(w, r)
			} else {
				http.Error(w, "Account unavailable", 503)
			}
			return
		}
		reply(w, account)
	}))
	mux.HandleFunc("GET /api/conversations", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if config.Sync == nil {
			reply(w, []any{})
			return
		}
		folder := r.URL.Query().Get("folder")
		if folder == "" {
			folder = "INBOX"
		}
		messages, err := config.Sync.Messages(r.Context(), s.User.ID, r.URL.Query().Get("account"), folder)
		if err != nil {
			if errors.Is(err, accounts.ErrNotFound) {
				http.NotFound(w, r)
			} else {
				http.Error(w, "Messages unavailable", 503)
			}
			return
		}
		reply(w, mailsync.Conversations(messages))
	}))
	mux.HandleFunc("POST /auth/logout", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		cookie, _ := r.Cookie(cookieName)
		if err := store.RevokeSession(r.Context(), cookie.Value); err != nil {
			http.Error(w, "Logout failed", 503)
			return
		}
		clearCookie(w)
		http.Redirect(w, r, "/login", 303)
	}))
	mux.HandleFunc("POST /auth/logout-all", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if !mutation(w, r, s) {
			return
		}
		if err := store.RevokeAll(r.Context(), s.User.ID); err != nil {
			http.Error(w, "Session revocation failed", 503)
			return
		}
		clearCookie(w)
		http.Redirect(w, r, "/login", 303)
	}))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	homeHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/settings" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		s, err := sessionFor(r)
		if err != nil {
			if errors.Is(err, identity.ErrNotFound) {
				http.Redirect(w, r, "/login", 303)
			} else {
				http.Error(w, "Session service unavailable", 503)
			}
			return
		}
		accounts, err := store.Accounts(r.Context(), s.User.ID)
		if err != nil {
			http.Error(w, "Accounts unavailable", 503)
			return
		}

		names := []webui.AccountSummary{}
		for _, a := range accounts {
			names = append(names, webui.AccountSummary{ID: a.ID, Name: a.Name})
		}
		message := ""
		switch r.URL.Query().Get("notice") {
		case "account-connected":
			message = "Mail server connection verified and saved. Open the mailbox and refresh to download messages."
		case "account-checked":
			message = "IMAP and SMTP connections verified."
		}
		render(w, "identity-home", webui.AuthPageData{Email: s.User.Email, CSRF: s.CSRF, Accounts: names, MailAccountsAvailable: config.Accounts != nil, MailEndpointsEnabled: config.MailEndpointsEnabled, Message: message})
	}
	mux.HandleFunc("/", homeHandler)
	mux.HandleFunc("GET /settings", homeHandler)
	// Preserve same-origin form Origin headers without sending referrers externally.
	return securityHeadersWithReferrer(mux, "same-origin"), nil
}
