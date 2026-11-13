// SPDX-License-Identifier: AGPL-3.0-only
package httpapi

import (
	"errors"
	"fmt"
	"mailspace.local/mailspace/internal/decision"
	"strings"
	"testing"
)

func TestClassificationFailuresAreSpecificAndNeverEchoProviderData(t *testing.T) {
	seen := map[string]bool{}
	for _, err := range []error{decision.ErrBusy, decision.ErrRateLimited, decision.ErrChanged, decision.ErrProvider, decision.ErrInput} {
		kind := decisionFailureNotice(fmt.Errorf("private message content: %w", err))
		message := decisionFailureMessage(kind)
		if seen[kind] || strings.Contains(message, "private message") || message == "" {
			t.Fatal("unsafe or ambiguous error category", kind)
		}
		seen[kind] = true
	}
	if decisionFailureNotice(errors.New("private provider response")) != "failed" {
		t.Fatal("unknown error leaked")
	}
}
