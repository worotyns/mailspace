// SPDX-License-Identifier: AGPL-3.0-only
package webui

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
)

// PreviewStyleHash permits only the fixed stylesheet produced by the asset build,
// never a style attribute/tag supplied by an email. Sandboxed source documents inherit this CSP.
func PreviewStyleHash(dev bool) (string, error) {
	var data []byte
	var err error
	if dev {
		data, err = os.ReadFile("internal/webui/assets/crypto/preview.css")
	} else {
		data, err = bundled.ReadFile("assets/crypto/preview.css")
	}
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return "'sha256-" + base64.StdEncoding.EncodeToString(hash[:]) + "'", nil
}
