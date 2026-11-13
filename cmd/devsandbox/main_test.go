// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSandboxLoginIsRandomPrivateAndNeverRegeneratedForExistingDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if _, err := privatePassword(path, true); err == nil {
		t.Fatal("missing login file regenerated for existing state")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed closed check created a file")
	}
	first, err := privatePassword(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 16 {
		t.Fatal("weak sandbox password")
	}
	second, err := privatePassword(path, true)
	if err != nil || second != first {
		t.Fatal("restart changed sandbox login")
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = privatePassword(path, true); err == nil {
		t.Fatal("public password file accepted")
	}
}
