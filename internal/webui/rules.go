// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/rules"
	"net/http"
)

type RulesData struct {
	CSRF    string
	Rules   []rules.Rule
	Spaces  []collections.Item
	Edit    *rules.Rule
	Preview *rules.Preview
	Notice  string
}

func (d RulesData) SpaceName(id string) string {
	for _, space := range d.Spaces {
		if space.ID == id {
			return space.Name
		}
	}
	return "Removed Space"
}
func RenderRules(w http.ResponseWriter, dev bool, data RulesData) error {
	return renderTimelinePart(w, dev, "rules-page", data)
}
