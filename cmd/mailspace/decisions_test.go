// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifierTokenSourcesAndDisabledMode(t *testing.T) {
	for _, key := range []string{"MAILSPACE_JEV_PROVIDER", "MAILSPACE_JEV_MODEL", "MAILSPACE_JEV_KEY_FILE", "MAILSPACE_JEV_API_TOKEN", "MAILSPACE_JEV_ENDPOINT"} {
		t.Setenv(key, "")
	}
	if service, err := decisions(nil, nil, nil, nil); err != nil || service != nil {
		t.Fatal("disabled provider initialized dependencies")
	}
	t.Setenv("MAILSPACE_JEV_PROVIDER", "openrouter")
	t.Setenv("MAILSPACE_JEV_MODEL", "synthetic/model")
	t.Setenv("MAILSPACE_JEV_API_TOKEN", "synthetic-token")
	// Both sources must fail before any store/provider access.
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("synthetic-file-token"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAILSPACE_JEV_KEY_FILE", path)
	if _, err := decisions(nil, nil, nil, nil); err == nil {
		t.Fatal("ambiguous token sources accepted")
	}
	t.Setenv("MAILSPACE_JEV_API_TOKEN", "")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := decisions(nil, nil, nil, nil); err == nil {
		t.Fatal("world-readable key accepted")
	}
}
