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
		return nil, errors.New("MAILSPACE_JEV_PROVIDER must be openrouter or local; leave empty to disable classification")
	}
	model := os.Getenv("MAILSPACE_JEV_MODEL")
	if strings.TrimSpace(model) == "" || model != strings.TrimSpace(model) || len(model) > 200 || strings.ContainsAny(model, "\r\n\x00") {
		return nil, errors.New("MAILSPACE_JEV_MODEL must be a nonempty model identifier without surrounding whitespace (maximum 200 bytes)")
	}
	if kind == "local" {
		endpoint = os.Getenv("MAILSPACE_JEV_ENDPOINT")
		if endpoint == "" {
			return nil, errors.New("MAILSPACE_JEV_ENDPOINT is required when MAILSPACE_JEV_PROVIDER=local")
		}
	}
	key := os.Getenv("MAILSPACE_JEV_API_TOKEN")
	if len(key) > 4096 || strings.ContainsAny(key, "\r\n\x00") {
		return nil, errors.New("MAILSPACE_JEV_API_TOKEN must be at most 4096 bytes without line breaks or NUL")
	}
	if path := os.Getenv("MAILSPACE_JEV_KEY_FILE"); path != "" {
		if key != "" {
			return nil, errors.New("set MAILSPACE_JEV_API_TOKEN or MAILSPACE_JEV_KEY_FILE, not both")
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
			return nil, errors.New("MAILSPACE_JEV_KEY_FILE must reference a readable regular file with private permissions (0600), at most 4 KiB")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, errors.New("MAILSPACE_JEV_KEY_FILE cannot be read by the app; check its container mount and ownership")
		}
		raw, err := io.ReadAll(io.LimitReader(file, 4097))
		file.Close()
		if err != nil || len(raw) > 4096 {
			return nil, errors.New("MAILSPACE_JEV_KEY_FILE cannot be read by the app; check its container mount and ownership")
		}
		key = strings.TrimSpace(string(raw))
		clear(raw)
	}
	if kind == "openrouter" && strings.TrimSpace(key) == "" {
		return nil, errors.New("OpenRouter requires MAILSPACE_JEV_API_TOKEN or a nonempty MAILSPACE_JEV_KEY_FILE; ensure Compose forwards the variable and recreate the container, or leave MAILSPACE_JEV_PROVIDER empty to disable classification")
	}
	if strings.ContainsAny(key, "\r\n\x00") {
		return nil, errors.New("JEV API token must not contain embedded line breaks or NUL")
	}
	var provider decision.Provider
	var err error
	if kind == "openrouter" {
		if strings.HasPrefix(model, "typesafe-ai/jev") {
			return nil, errors.New("MAILSPACE_JEV_MODEL uses the wrong OpenRouter namespace: use an exact typesafe/jev model ID from the OpenRouter catalog, for example typesafe/jev-1.13")
		}
		provider, err = decision.NewOpenRouterProvider(model, key)
	} else {
		provider, err = decision.NewChatProvider(endpoint, model, key, false)
	}
	if err != nil {
		return nil, errors.New("invalid MAILSPACE_JEV_ENDPOINT: use a full HTTPS chat-completions URL without credentials, query or fragment; HTTP is allowed only on numeric loopback for a local provider")
	}
	sources := timeline.New(store, a, syncer, box)
	return decision.New(store.Database(), sources, collections.New(store.Database(), sources), provider), nil
}
