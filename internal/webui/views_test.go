// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"mailspace.local/mailspace/internal/collections"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMobileViewSwitcherUsesScopedFreshLinksAndNativeFallback(t *testing.T) {
	data := TimelineData{Title: "Current project", Filter: "unsorted", AccountID: "account-one", LabelID: "old-status", SpaceID: "old-space", Query: "old search", ViewsOpen: true,
		Spaces: []CollectionView{{Item: collections.Item{ID: "project", Name: "User project <unsafe>", Kind: "space", Color: "green"}}},
		Labels: []CollectionView{{Item: collections.Item{ID: "custom-status", Name: "User status", Kind: "label", Color: "blue"}}},
	}
	response := httptest.NewRecorder()
	if err := RenderTimeline(response, httptest.NewRequest("GET", "/views", nil), false, data); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	for _, want := range []string{`data-open-dialog="view-switcher"`, `aria-controls="view-switcher"`, `aria-label="Change view: Current project"`, `id="view-switcher"`, `aria-label="Choose timeline view"`, `method="dialog"`, `User project &lt;unsafe&gt;`, `href="/?account=account-one&amp;space=project"`, `href="/?account=account-one&amp;label=custom-status"`, `href="/?account=account-one&amp;filter=all"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if !strings.HasPrefix(data.ViewsURL(), "/views?") {
		t.Fatal("missing no-JavaScript fallback")
	}
	if data.ViewLink("space", "project") != "/?account=account-one&space=project" {
		t.Fatal("new view retained unrelated filters")
	}
	if strings.Contains(html, `role="menuitem"`) {
		t.Fatal("navigation must keep normal link keyboard behavior")
	}
}
