// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"bytes"
	"net/http"
)

type AccountSummary struct {
	Address string
	ID      string
	Name    string
}
type AccountForm struct {
	SentMailbox     string
	ID              string
	Name            string
	Address         string
	IMAPHost        string
	IMAPPort        int
	IMAPTLS         string
	IMAPUser        string
	SMTPHost        string
	SMTPPort        int
	SMTPTLS         string
	SMTPUser        string
	SameCredentials bool
}
type AuthPageData struct {
	OrdinaryRead          bool
	SecureRead            bool
	RawURL                string
	ClaimedSender         string
	UserID                string
	SessionExpires        int64
	ProfileName           string
	Timezone              string
	Workspace             WorkspaceSummary
	Workspaces            []WorkspaceSummary
	Members               []MemberSummary
	Compose               ComposeForm
	Outgoing              []OutgoingRecord
	AccountID             string
	AccountName           string
	OutgoingID            string
	MailAccountsAvailable bool
	MailEndpointsEnabled  bool
	Form                  AccountForm
	Email                 string
	Message               string
	CSRF                  string
	Accounts              []AccountSummary
}

func RenderAuth(w http.ResponseWriter, dev bool, name string, data AuthPageData) error {
	templates, err := Templates(dev)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	if err = templates.ExecuteTemplate(&body, name, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err = w.Write(body.Bytes())
	return err
}

type ComposeForm struct {
	AccountID      string
	AccountName    string
	Address        string
	Nonce          string
	To             string
	CC             string
	BCC            string
	Subject        string
	Body           string
	AllowPlaintext bool
	ReplyFolder    string
	ReplyValidity  uint32
	ReplyUID       uint32
}
type OutgoingRecord struct {
	ID        string
	Subject   string
	State     string
	CopyState string
	CreatedAt string
}

type WorkspaceSummary struct {
	ID     string
	Name   string
	Role   string
	Active bool
}
type MemberSummary struct {
	UserID string
	Name   string
	Email  string
	Owner  bool
}
