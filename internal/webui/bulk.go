// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"mailspace.local/mailspace/internal/collections"
	"net/http"
)

type BulkData struct {
	Labels                                    bool
	Items                                     []collections.Item
	CSRF, Scope, Key, Person, Reference, Hash string
	Count, Total                              int
}

func RenderBulk(w http.ResponseWriter, dev bool, data BulkData) error {
	return renderTimelinePart(w, dev, "bulk-page", data)
}
func RenderLabels(w http.ResponseWriter, dev bool, data BulkData) error {
	return renderTimelinePart(w, dev, "labels-page", data)
}
