// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"os"
	"strings"
)

// flagWritesDisabled reports whether the bounded provider \Seen capability was
// explicitly turned off for this installation. It is on by default; a disabled
// capability performs no remote flag call at all.
func flagWritesDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAILSPACE_IMAP_FLAG_WRITES"))) {
	case "0", "false", "off", "no":
		return true
	}
	return false
}
