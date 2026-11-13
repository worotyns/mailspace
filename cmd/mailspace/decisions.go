// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"errors"
	"io"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/decision"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"mailspace.local/mailspace/internal/timeline"
	"os"
	"strings"
)

func decisions(store *identity.Store, a *accounts.Service, syncer *mailsync.Service, box *outbox.Service) (*decision.Service, error) {
	kind := os.Getenv("MAILSPACE_JEV_PROVIDER")
	if kind == "" {
		return nil, nil
	}
	endpoint := "https://openrouter.ai/api/v1/chat/completions"
	if kind != "openrouter" && kind != "local" {
		return nil, errors.New("JEV provider must be openrouter or local")
	}
	if kind == "local" {
		endpoint = os.Getenv("MAILSPACE_JEV_ENDPOINT")
	}
	key := os.Getenv("MAILSPACE_JEV_API_TOKEN")
	if len(key) > 4096 || strings.ContainsAny(key, "\r\n\x00") {
		return nil, errors.New("invalid JEV token format")
	}
	if path := os.Getenv("MAILSPACE_JEV_KEY_FILE"); path != "" {
		if key != "" {
			return nil, errors.New("set JEV API_TOKEN or KEY_FILE, not both")
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
			return nil, errors.New("JEV key file must be a private regular file under 4 KiB")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, errors.New("JEV key unavailable")
		}
		raw, err := io.ReadAll(io.LimitReader(file, 4097))
		file.Close()
		if err != nil || len(raw) > 4096 {
			return nil, errors.New("JEV key unavailable")
		}
		key = strings.TrimSpace(string(raw))
		clear(raw)
	}
	provider, err := decision.NewChatProvider(endpoint, os.Getenv("MAILSPACE_JEV_MODEL"), key, kind == "openrouter")
	if err != nil {
		return nil, err
	}
	sources := timeline.New(store, a, syncer, box)
	return decision.New(store.Database(), sources, collections.New(store.Database(), sources), provider), nil
}
