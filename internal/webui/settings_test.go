// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestSettingsPagesShareNavigationAndStableContentWrapper(t *testing.T) {
	tests := []struct {
		name, section string
		data          any
	}{
		{"identity-home", "/settings", AuthPageData{}},
		{"account-settings", "/settings", AuthPageData{}},
		{"profile", "/profile", AuthPageData{}},
		{"security", "/security", AuthPageData{}},
		{"keys", "/keys", AuthPageData{}},
		{"workspaces", "/workspaces", AuthPageData{}},
		{"workspace-members", "/workspaces", AuthPageData{}},
		{"rules-page", "/rules", RulesData{}},
		{"automation-page", "/automation", AutomationData{}},
		{"labels-page", "/labels", BulkData{}},
	}
	current := regexp.MustCompile(`<a\s+href="([^"]+)"\s+aria-current="page"`)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			if err := renderTimelinePart(response, false, test.name, test.data); err != nil {
				t.Fatal(err)
			}
			html := response.Body.String()
			mainAt, navAt := strings.Index(html, "<main"), strings.Index(html, `aria-label="Settings sections"`)
			if mainAt < 0 || navAt < mainAt || !strings.Contains(html, "settings-main") || !strings.Contains(html, "settings-document") {
				t.Fatal("settings navigation must share the main content footprint")
			}
			if strings.Count(html, `aria-label="Settings sections"`) != 1 {
				t.Fatal("settings navigation repeated")
			}
			links := current.FindAllStringSubmatch(html, -1)
			if len(links) != 1 || links[0][1] != test.section {
				t.Fatalf("wrong current section: %v", links)
			}
			if !strings.Contains(html, `href="/" class="settings-back"`) {
				t.Fatal("native timeline fallback missing")
			}
		})
	}
}

func TestLoginBrandEmphasizesMailOnly(t *testing.T) {
	response := httptest.NewRecorder()
	if err := RenderAuth(response, false, "login", AuthPageData{}); err != nil {
		t.Fatal(err)
	}
	html := response.Body.String()
	if !strings.Contains(html, `<strong class="font-bold">mail</strong>`) || !strings.Contains(html, `<span class="font-normal">space</span>`) || strings.Contains(html, "mailspace.</a") {
		t.Fatal("brand must use bold mail and normal space without punctuation")
	}
	if strings.Contains(html, "settings-navigation") {
		t.Fatal("settings navigation leaked into sign-in")
	}
}

func TestSecurityNavigationStaysInPanelAndSignalsOnlyRotatedSessions(t *testing.T) {
	for _, rotated := range []bool{false, true} {
		response := httptest.NewRecorder()
		if err := RenderAuth(response, false, "security", AuthPageData{Security: SecurityPageData{SessionRotated: rotated}}); err != nil {
			t.Fatal(err)
		}
		link := regexp.MustCompile(`<a\s+href="/security"[^>]*>`).FindString(response.Body.String())
		if link == "" || strings.Contains(link, `target="_top"`) {
			t.Fatal("security navigation escapes its settings panel")
		}
		if strings.Contains(response.Body.String(), "data-session-rotated") != rotated {
			t.Fatal("session change marker missing or redisplayed on normal GET")
		}
	}
}
