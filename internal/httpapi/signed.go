// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/outbox"
)

// A dedicated authored-only contract. It accepts no key, passphrase, decrypted
// message, body fallback, client envelope header or generic export destination.
func signedRoutes(mux *http.ServeMux, config AuthConfig, protected protectedRoute, mutation func(http.ResponseWriter, *http.Request, identity.Session) bool, budget func(http.ResponseWriter, *http.Request, string) bool, release func()) {
	mux.HandleFunc("POST /api/outbox/signed", protected(func(w http.ResponseWriter, r *http.Request, s identity.Session) {
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
			Account        string `json:"account"`
			Nonce          string `json:"requestId"`
			To             string `json:"to"`
			CC             string `json:"cc"`
			BCC            string `json:"bcc"`
			Subject        string `json:"subject"`
			SignedMIME     string `json:"signedMime"`
			Provenance     string `json:"provenance"`
			AllowPlaintext bool   `json:"allowPlaintext"`
			ReplyFolder    string `json:"replyFolder"`
			ReplyValidity  uint32 `json:"replyValidity"`
			ReplyUID       uint32 `json:"replyUid"`
			Reference      string `json:"reference"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.SignedMIME == "" || input.Provenance != "authored" || !input.AllowPlaintext {
			http.Error(w, "Authored signed MIME and explicit unencrypted-send consent required", 400)
			return
		}
		if !budget(w, r, s.User.ID) {
			return
		}
		defer release()
		draft := outbox.Draft{To: input.To, CC: input.CC, BCC: input.BCC, Subject: input.Subject, SignedMIME: input.SignedMIME, Provenance: input.Provenance, AllowPlaintext: input.AllowPlaintext, ReplyFolder: input.ReplyFolder, ReplyValidity: input.ReplyValidity, ReplyUID: input.ReplyUID}
		var record outbox.Record
		var err error
		if input.Reference != "" {
			ref, e := decodeSource(input.Reference)
			err = e
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
			http.Error(w, "Unable to queue signed mail; inspect consent, source and Outbox before retrying", 422)
			return
		}
		_ = config.Outbox.Dispatch(r.Context(), s.User.ID, record.ID)
		current, err := config.Outbox.Get(r.Context(), s.User.ID, record.ID)
		state, copyState := "unknown", "unknown"
		if err == nil {
			state, copyState = current.State, current.CopyState
		}
		reply(w, map[string]string{"id": record.ID, "state": state, "copyState": copyState, "location": "/outbox?" + url.Values{"account": {record.AccountID}, "message": {record.ID}}.Encode()})
	}))
}
