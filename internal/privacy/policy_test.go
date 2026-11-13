// SPDX-License-Identifier: AGPL-3.0-only
package privacy

import "testing"

func TestExportBoundary(t *testing.T) {
	for _, d := range []Destination{ServerIndex, AI, Webhook, "unknown"} {
		for _, c := range []ContentClass{Decrypted, Ciphertext, "unknown"} {
			if AllowExport(c, d, true, true) {
				t.Fatalf("exported %s to %s", c, d)
			}
		}
	}
	for _, d := range []Destination{ServerIndex, AI, Webhook} {
		for _, c := range []ContentClass{Metadata, Plaintext} {
			for _, flags := range [][2]bool{{false, false}, {true, false}, {false, true}} {
				if AllowExport(c, d, flags[0], flags[1]) {
					t.Fatal("missing opt-in accepted")
				}
			}
			if !AllowExport(c, d, true, true) {
				t.Fatal("explicit ordinary-content opt-in rejected")
			}
		}
	}
}
