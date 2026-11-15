// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"bytes"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
)

type AccountSummary struct {
	Address string
	ID      string
	Name    string
}
type AccountForm struct {
	ImportMode      string
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
type SecurityPageData struct {
	Enabled, Pending, RecoveryMethod, SetupUnavailable, SessionRotated bool
	Remaining                                                          int
	EnrollmentSecret                                                   string
	RecoveryCodes                                                      []string
}

func (SecurityPageData) String() string   { return "<redacted security page>" }
func (SecurityPageData) GoString() string { return "<redacted security page>" }

type AuthPageData struct {
	Security              SecurityPageData
	Icons                 template.HTML
	ClassifyReference     string
	ClassifyProvider      string
	OrdinaryRead          bool
	SecureRead            bool
	RawURL                string
	ToLabel               string
	CcLabel               string
	ReadOnly              bool
	SharedBy              string
	DownloadAllowed       bool
	SeenReference         string
	SeenWrites            bool
	SeenUnread            bool
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
	icons, err := bundled.ReadFile("assets/icons.svg")
	if err != nil {
		return err
	}
	// Only the fixed application icon asset is trusted HTML, never mail content.
	data.Icons = template.HTML(icons)
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
	AccountID          string
	AccountName        string
	Address            string
	Nonce              string
	To                 string
	CC                 string
	BCC                string
	Subject            string
	Body               string
	AllowPlaintext     bool
	EncryptionRequired bool
	ReplyFolder        string
	ReplyValidity      uint32
	ReplyUID           uint32
}

// SecureURL carries the selected original, never message text or decrypted data.
func (f ComposeForm) SecureURL() string {
	query := url.Values{"account": {f.AccountID}}
	if f.ReplyUID != 0 {
		query.Set("folder", f.ReplyFolder)
		query.Set("validity", strconv.FormatUint(uint64(f.ReplyValidity), 10))
		query.Set("uid", strconv.FormatUint(uint64(f.ReplyUID), 10))
	}
	return "/secure/compose?" + query.Encode()
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
