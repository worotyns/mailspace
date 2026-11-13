// SPDX-License-Identifier: AGPL-3.0-only
package privacy

// ClientClassificationConsent is scoped to one user-initiated selected excerpt.
// It is not permission for generic indexing, tracing, webhooks or background AI.
type ClientClassificationConsent struct {
	Provider string `json:"provider"`
	Purpose  string `json:"purpose"`
	Approved bool   `json:"approved"`
}

func (c ClientClassificationConsent) Allows(provider string, bytes int) bool {
	return c.Approved && provider != "" && c.Provider == provider && c.Purpose == "classify-selected-excerpt" && bytes > 0 && bytes <= 4096
}
