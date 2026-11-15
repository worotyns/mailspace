// SPDX-License-Identifier: AGPL-3.0-only
package webui

import "net/http"

// GrantSummary is one owner-visible source grant.
type GrantSummary struct {
	ID          string
	Workspace   string
	Account     string
	Mailbox     string
	Grantee     string
	CanDownload bool
}

type SharingData struct {
	CSRF       string
	Notice     string
	Error      string
	Workspaces []WorkspaceSummary
	Accounts   []AccountSummary
	Grants     []GrantSummary
}

func RenderSharing(w http.ResponseWriter, dev bool, data SharingData) error {
	return renderTimelinePart(w, dev, "sharing-page", data)
}
