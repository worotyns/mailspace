// SPDX-License-Identifier: AGPL-3.0-only
package privacy

import "testing"

func TestSelectionConsentDoesNotOpenGenericExports(t *testing.T) {
	consent := ClientClassificationConsent{Provider: "configured-provider", Purpose: "classify-selected-excerpt", Approved: true}
	if !consent.Allows("configured-provider", 100) {
		t.Fatal("explicit selection denied")
	}
	if consent.Allows("another-provider", 100) || consent.Allows("configured-provider", 0) || consent.Allows("configured-provider", 4097) {
		t.Fatal("selection scope expanded")
	}
	consent.Purpose = "index-all-mail"
	if consent.Allows("configured-provider", 100) {
		t.Fatal("unknown purpose allowed")
	}
	for _, destination := range []Destination{AI, Webhook, ServerIndex} {
		if AllowExport(Decrypted, destination, true, true) {
			t.Fatal("generic decrypted export unlocked")
		}
	}
}
