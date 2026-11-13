// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"mailspace.local/mailspace/internal/decision"
	"net/http"
	"net/url"
)

type AutomationData struct {
	Email      string
	CSRF       string
	Available  bool
	Provider   string
	Settings   decision.Settings
	Runs       []decision.Run
	SpaceNames map[string]string
	Notice     string
}

func RenderAutomation(w http.ResponseWriter, dev bool, data AutomationData) error {
	return renderTimelinePart(w, dev, "automation-page", data)
}

func (d AutomationData) SpaceName(id string) string {
	if name := d.SpaceNames[id]; name != "" {
		return name
	}
	return "Removed/unknown Space"
}
func (d AutomationData) SourceURL(run decision.Run) string {
	return "/messages/read?" + url.Values{"reference": {EncodeSource(run.Reference())}}.Encode()
}
