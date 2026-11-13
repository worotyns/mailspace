// SPDX-License-Identifier: AGPL-3.0-only
package mail

import "testing"

func TestSyntheticSeed(t *testing.T) {
	if err := ValidateDemo(); err != nil {
		t.Fatal(err)
	}
	seed := Demo()
	if len(seed) != 7 {
		t.Fatal("unexpected seed size")
	}
	messages, encrypted, attachments := 0, 0, 0
	for _, c := range seed {
		for _, m := range c.Messages {
			messages++
			attachments += len(m.Attachments)
			if m.Encrypted {
				encrypted++
			}
		}
	}
	if messages != 17 || encrypted != 1 || attachments != 4 {
		t.Fatalf("incomplete fixture: %d messages, %d encrypted, %d attachments", messages, encrypted, attachments)
	}
	seed[0].Messages[0].Text = "changed by request"
	if Demo()[0].Messages[0].Text == seed[0].Messages[0].Text {
		t.Fatal("seed shared mutable state")
	}
}
