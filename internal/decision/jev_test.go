// SPDX-License-Identifier: AGPL-3.0-only
package decision

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

type decisionRoundTripper func(*http.Request) (*http.Response, error)

func (f decisionRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestJevDecisionsContractManySpacesAndConservativeConfidence(t *testing.T) {
	provider, err := NewOpenRouterProvider("typesafe/jev-1.13", "synthetic-token")
	if err != nil {
		t.Fatal(err)
	}
	jev, ok := provider.(*JevProvider)
	if !ok {
		t.Fatal("Jev incorrectly routed to chat")
	}
	calls := 0
	jev.transport.client.Transport = decisionRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != openRouterDecisions || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("wrong Jev endpoint/auth")
		}
		var request map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Fatal("invalid JSON")
		}
		for _, forbidden := range []string{"messages", "tools", "response_format", "trace", "user", "session_id", "assets"} {
			if _, exists := request[forbidden]; exists {
				t.Fatal("unexpected field", forbidden)
			}
		}
		var model string
		json.Unmarshal(request["model"], &model)
		if model != "typesafe/jev-1.13" {
			t.Fatal("model silently substituted")
		}
		var policy map[string]any
		json.Unmarshal(request["provider"], &policy)
		if policy["data_collection"] != "deny" || policy["zdr"] != true || policy["require_parameters"] != true || policy["allow_fallbacks"] != false {
			t.Fatal("privacy policy weakened")
		}
		var questions map[string]struct {
			Type, Instructions string
			Criteria           map[string]string
		}
		json.Unmarshal(request["questions"], &questions)
		if len(questions) != 3 || questions["payments"].Type != "noul" || !strings.Contains(questions["payments"].Instructions, "Payment brief") || questions["payments"].Criteria["false"] == "" {
			t.Fatal("wrong independent relevance questions")
		}
		var state map[string]any
		json.Unmarshal(request["state"], &state)
		if state["text"] != "Approved synthetic excerpt" || state["subject"] != "" {
			t.Fatal("wrong state")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"answers":{"payments":{"type":"noul","noul":0.97},"commerce":{"type":"noul","noul":0.83},"personal":{"type":"noul","noul":0.1}},"usage":{"input_tokens":100},"model":"typesafe/jev-1.13"}`)), Header: make(http.Header)}, nil
	})
	input := Input{Text: "Approved synthetic excerpt", Spaces: []Definition{{ID: "payments", Name: "Payments", Brief: "Payment brief"}, {ID: "commerce", Name: "Commerce"}, {ID: "personal", Name: "Personal"}}}
	proposal, err := jev.Propose(context.Background(), input)
	if err != nil || calls != 1 || len(proposal.SpaceIDs) != 2 || proposal.SpaceIDs[0] != "payments" || proposal.SpaceIDs[1] != "commerce" || math.Abs(proposal.Confidence-.83) > 1e-9 {
		t.Fatalf("wrong multi-space result %+v %v", proposal, err)
	}
	if strings.Contains(jev.String(), "synthetic-token") {
		t.Fatal("key in logs")
	}
	chat, err := NewOpenRouterProvider("synthetic/ordinary-model", "synthetic-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := chat.(*ChatProvider); !ok {
		t.Fatal("ordinary chat compatibility lost")
	}
	router, err := NewOpenRouterProvider("typesafe/jev-router", "synthetic-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := router.(*ChatProvider); !ok {
		t.Fatal("Jev Router treated as decision model")
	}
}
func TestJevRejectsIncompleteOrInvalidAnswersAndHoldsUncertainty(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
		confidence float64
		selected   int
	}{
		{"missing", `{"answers":{}}`, false, 0, 0},
		{"foreign", `{"answers":{"foreign":{"type":"noul","noul":1}}}`, false, 0, 0},
		{"missing probability", `{"answers":{"space":{"type":"noul"}}}`, false, 0, 0},
		{"null probability", `{"answers":{"space":{"type":"noul","noul":null}}}`, false, 0, 0},
		{"wrong type", `{"answers":{"space":{"type":"choice","noul":1}}}`, false, 0, 0},
		{"out of range", `{"answers":{"space":{"type":"noul","noul":1.1}}}`, false, 0, 0},
		{"negative", `{"answers":{"space":{"type":"noul","noul":-0.1}}}`, false, 0, 0},
		{"uncertain", `{"answers":{"space":{"type":"noul","noul":0.55}}}`, true, .55, 1},
		{"unrelated", `{"answers":{"space":{"type":"noul","noul":0.05}}}`, true, .95, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, err := NewOpenRouterProvider("typesafe/jev-1.13", "synthetic-token")
			if err != nil {
				t.Fatal(err)
			}
			jev := provider.(*JevProvider)
			jev.transport.client.Transport = decisionRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			result, err := jev.Propose(context.Background(), Input{Spaces: []Definition{{ID: "space"}}})
			if !tc.valid {
				if err == nil {
					t.Fatal("invalid answer accepted")
				}
				return
			}
			if err != nil || len(result.SpaceIDs) != tc.selected || math.Abs(result.Confidence-tc.confidence) > 1e-9 {
				t.Fatalf("wrong uncertainty handling %+v %v", result, err)
			}
		})
	}
}
