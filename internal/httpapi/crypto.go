// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mail"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"mailspace.local/mailspace/internal/timeline"
	"mailspace.local/mailspace/internal/webui"
)

type protectedRoute func(func(http.ResponseWriter, *http.Request, identity.Session)) http.HandlerFunc

func renderSecurePage(w http.ResponseWriter, config AuthConfig, data webui.AuthPageData) {
	framePolicy := "frame-src 'none'; frame-ancestors 'none';"
	stylePolicy := "style-src 'self';"
	if data.SecureRead {
		hash, err := webui.PreviewStyleHash(config.Dev)
		if err != nil {
			http.Error(w, "Preview assets unavailable", 503)
			return
		}
		stylePolicy = "style-src 'self' " + hash + ";"
		// The source document stays client-owned in an opaque sandbox. Only
		// the exact fixed stylesheet is permitted, never styles supplied by mail.
		framePolicy = "frame-src 'self'; frame-ancestors 'none';"
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; "+stylePolicy+" img-src 'none'; connect-src 'self'; "+framePolicy+" base-uri 'none'; form-action 'none'")
	if err := webui.RenderAuth(w, config.Dev, "secure-mail", data); err != nil {
		http.Error(w, "Unable to render secure mail", 500)
	}
}
func cryptoRoutes(mux *http.ServeMux, config AuthConfig, store *identity.Store, protected protectedRoute, mutation func(http.ResponseWriter, *http.Request, identity.Session) bool, budget func(http.ResponseWriter, *http.Request, string) bool, release func()) {
	render := func(w http.ResponseWriter, data webui.AuthPageData) { renderSecurePage(w, config, data) }
	raw := func(r *http.Request, s identity.Session) ([]byte, error) {
		if config.Sync == nil {
			return nil, outbox.ErrNotFound
		}
		q := r.URL.Query()
		validity, e := strconv.ParseUint(q.Get("validity"), 10, 32)
		uid, e2 := strconv.ParseUint(q.Get("uid"), 10, 32)
		if e != nil || e2 != nil || validity == 0 || uid == 0 {
			return nil, outbox.ErrInput
		}
		return config.Sync.Raw(r.Context(), s.User.ID, q.Get("account"), q.Get("folder"), uint32(validity), uint32(uid))
	}
	mux.HandleFunc("GET /api/mail/raw", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		data, err := raw(r, s)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="message.eml"`)
		_, _ = w.Write(data)
	}))
	mux.HandleFunc("GET /secure/read", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		data, err := raw(r, s)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		preview := mailsync.Parse(data)
		render(w, webui.AuthPageData{UserID: s.User.ID, SessionExpires: s.ExpiresAt.UnixMilli(), Email: s.User.Email, SecureRead: true, OrdinaryRead: !preview.Encrypted, RawURL: "/api/mail/raw?" + r.URL.Query().Encode(), ClaimedSender: preview.Sender, AccountID: r.URL.Query().Get("account")})
	}))
	mux.HandleFunc("GET /secure/compose", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if config.Accounts == nil || config.Outbox == nil {
			http.Error(w, "Compose unavailable", 503)
			return
		}
		account, err := config.Accounts.Get(r.Context(), s.User.ID, r.URL.Query().Get("account"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		form := webui.ComposeForm{AccountID: account.ID, Address: account.Address, AccountName: account.Name, Nonce: rand.Text()}
		if r.URL.Query().Get("uid") != "" {
			uid, e := strconv.ParseUint(r.URL.Query().Get("uid"), 10, 32)
			validity, e2 := strconv.ParseUint(r.URL.Query().Get("validity"), 10, 32)
			if e != nil || e2 != nil || uid == 0 || validity == 0 {
				http.Error(w, "Invalid reply", 400)
				return
			}
			parent, err := config.Outbox.Parent(r.Context(), s.User.ID, account.ID, r.URL.Query().Get("folder"), uint32(validity), uint32(uid))
			if err != nil {
				http.NotFound(w, r)
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
		render(w, webui.AuthPageData{UserID: s.User.ID, SessionExpires: s.ExpiresAt.UnixMilli(), Email: s.User.Email, CSRF: s.CSRF, AccountID: account.ID, Compose: form})
	}))
	mux.HandleFunc("POST /api/outbox/encrypted", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
		if config.Outbox == nil {
			http.Error(w, "Sending unavailable", 503)
			return
		}
		if !mutation(w, r, s) {
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "JSON required", 415)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
		var input struct {
			Account       string `json:"account"`
			Nonce         string `json:"requestId"`
			To            string `json:"to"`
			CC            string `json:"cc"`
			BCC           string `json:"bcc"`
			Subject       string `json:"subject"`
			EncryptedMIME string `json:"encryptedMime"`
			ReplyFolder   string `json:"replyFolder"`
			ReplyValidity uint32 `json:"replyValidity"`
			ReplyUID      uint32 `json:"replyUid"`
			Reference     string `json:"reference"`
			Space         string `json:"space"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.EncryptedMIME == "" {
			http.Error(w, "Invalid encrypted submission", 400)
			return
		}
		var collectionService *collections.Service
		if input.Space != "" {
			if config.Accounts == nil || config.Sync == nil {
				http.Error(w, "Space source unavailable", 503)
				return
			}
			collectionService = collections.New(store.Database(), timeline.New(store, config.Accounts, config.Sync, config.Outbox))
			if _, err := collectionService.Get(r.Context(), s.User.ID, "space", input.Space); err != nil {
				http.NotFound(w, r)
				return
			}
		}
		if !budget(w, r, s.User.ID) {
			return
		}
		defer release()
		draft := outbox.Draft{To: input.To, CC: input.CC, BCC: input.BCC, Subject: input.Subject, EncryptedMIME: input.EncryptedMIME, ReplyFolder: input.ReplyFolder, ReplyValidity: input.ReplyValidity, ReplyUID: input.ReplyUID}
		var record outbox.Record
		var err error
		if input.Reference != "" {
			var ref mail.SourceRef
			ref, err = decodeSource(input.Reference)
			if err == nil && (input.ReplyFolder != "" || input.ReplyUID != 0 || input.ReplyValidity != 0 || ref.AccountID != input.Account) {
				err = outbox.ErrInput
			}
			if err == nil {
				record, err = config.Outbox.QueueReply(r.Context(), s.User.ID, input.Nonce, ref, draft)
			}
		} else {
			record, err = config.Outbox.Queue(r.Context(), s.User.ID, input.Account, input.Nonce, draft)
		}
		if err != nil {
			http.Error(w, "Unable to queue encrypted mail; inspect recipients, size and Outbox before retrying", 422)
			return
		}
		if collectionService != nil {
			account, e := config.Accounts.Get(r.Context(), s.User.ID, record.AccountID)
			if e == nil {
				// Failure cannot undo a queued SMTP attempt. Outbox remains the
				// authoritative result and the source can be assigned manually.
				_ = collectionService.Assign(r.Context(), s.User.ID, "space", input.Space, mail.SourceRef{Kind: "outbox", AccountID: account.ID, Revision: account.Revision, OutgoingID: record.ID}, true)
			}
		}
		_ = config.Outbox.Dispatch(r.Context(), s.User.ID, record.ID)
		reply(w, map[string]string{"location": "/outbox?" + url.Values{"account": {record.AccountID}, "message": {record.ID}}.Encode()})
	}))
}
