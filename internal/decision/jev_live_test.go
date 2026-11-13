// SPDX-License-Identifier: AGPL-3.0-only
package decision

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in only. Two synthetic requests, no retries, no production mail.
// The API token and raw response are never printed or included in assertions.
func TestLiveJevSynthetic(t *testing.T) {
	path := os.Getenv("MAILSPACE_LIVE_JEV_KEY_FILE")
	if path == "" {
		t.Skip("opt-in paid synthetic provider check")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		t.Fatal("private test key unavailable")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("private test key unreadable")
	}
	key := strings.TrimSpace(string(raw))
	clear(raw)
	provider, err := NewOpenRouterProvider("typesafe/jev-1.13", key)
	if err != nil {
		t.Fatal("invalid private test credentials")
	}
	spaces := []Definition{{ID: "payments", Name: "Payments", Brief: "Invoices, payment processing, settlements and publisher payments"}, {ID: "commerce", Name: "Commerce", Brief: "E-commerce loyalty programmes, affiliate partnerships and online retail"}, {ID: "personal", Name: "Personal", Brief: "Home, household utilities, cinema and private appointments"}}
	for _, tc := range []struct{ subject, text, want string }{{"November invoice", "Please settle invoice TEST-42 for payment processing services.", "payments"}, {"Cinema tickets", "Our cinema booking is confirmed for Saturday evening.", "personal"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		proposal, err := provider.Propose(ctx, Input{Spaces: spaces, From: "sender@example.test", To: []string{"recipient@example.test"}, Subject: tc.subject, Text: tc.text})
		cancel()
		if err != nil {
			t.Fatalf("synthetic Jev request failed: %v", err)
		}
		found := false
		for _, id := range proposal.SpaceIDs {
			if id == tc.want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected synthetic category %s, got category IDs %v", tc.want, proposal.SpaceIDs)
		}
		t.Logf("synthetic category=%s confidence=%.3f", tc.want, proposal.Confidence)
	}
}
