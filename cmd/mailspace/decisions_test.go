// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestClassifierStartupErrorsIdentifyMissingConfigurationWithoutSecrets(t *testing.T) {
	for _, tc := range []struct{ name, provider, model, token, endpoint, want string }{
		{"missing model", "openrouter", "", "synthetic-secret-canary", "", "MAILSPACE_JEV_MODEL"},
		{"missing token", "openrouter", "typesafe-ai/jev-1.3", "", "", "OpenRouter requires MAILSPACE_JEV_API_TOKEN"},
		{"missing local endpoint", "local", "synthetic/model", "", "", "MAILSPACE_JEV_ENDPOINT is required"},
		{"unsafe local endpoint", "local", "synthetic/model", "synthetic-secret-canary", "http://remote.example.test/v1/chat/completions", "invalid MAILSPACE_JEV_ENDPOINT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAILSPACE_JEV_PROVIDER", tc.provider)
			t.Setenv("MAILSPACE_JEV_MODEL", tc.model)
			t.Setenv("MAILSPACE_JEV_API_TOKEN", tc.token)
			t.Setenv("MAILSPACE_JEV_ENDPOINT", tc.endpoint)
			t.Setenv("MAILSPACE_JEV_KEY_FILE", "")
			_, err := decisions(nil, nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected useful config error, got %v", err)
			}
			if strings.Contains(err.Error(), "synthetic-secret-canary") {
				t.Fatal("configuration error exposed token")
			}
		})
	}
}
