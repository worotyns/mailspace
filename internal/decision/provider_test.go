// SPDX-License-Identifier: AGPL-3.0-only
package decision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderRequestAndResponseBoundary(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var input map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			t.Fatal("invalid request")
		}
		if _, ok := input["tools"]; ok {
			t.Fatal("model gained tools")
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-key" {
			t.Fatal("missing operator auth")
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"space_ids":["allowed"],"confidence":0.9}`}}}})
	}))
	defer server.Close()
	provider, err := NewChatProvider(server.URL, "synthetic-model", "synthetic-key", false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Propose(context.Background(), Input{Spaces: []Definition{{ID: "allowed", Brief: "Synthetic"}}, Subject: "Synthetic email"})
	if err != nil || len(result.SpaceIDs) != 1 || calls != 1 {
		t.Fatal("provider boundary failed", err)
	}
	if strings.Contains(provider.String(), "synthetic-key") {
		t.Fatal("provider logs key")
	}
	if _, err = NewChatProvider("http://external.example.test/v1", "model", "", false); err == nil {
		t.Fatal("plaintext remote endpoint allowed")
	}
	if _, err = NewChatProvider(server.URL, "model", "key", true); err == nil {
		t.Fatal("OpenRouter endpoint could be redirected")
	}
}
