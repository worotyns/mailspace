// SPDX-License-Identifier: AGPL-3.0-only
package privacy

// ContentClass tracks provenance, not whether bytes happen to look encrypted.
type ContentClass string
type Destination string

const (
	Metadata    ContentClass = "metadata"
	Plaintext   ContentClass = "plaintext"
	Ciphertext  ContentClass = "ciphertext"
	Decrypted   ContentClass = "client-decrypted"
	ServerIndex Destination  = "server-index"
	AI          Destination  = "ai"
	Webhook     Destination  = "webhook"
)

// AllowExport is deliberately fail-closed. Decrypted content has no server export
// path, even with consent. Future explicit client actions need a separate design.
func AllowExport(class ContentClass, dest Destination, enabled, explicitConsent bool) bool {
	if dest != ServerIndex && dest != AI && dest != Webhook {
		return false
	}
	if class != Metadata && class != Plaintext {
		return false
	}
	return enabled && explicitConsent
}
