// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/collections"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mailsync"
	"mailspace.local/mailspace/internal/outbox"
	"mailspace.local/mailspace/internal/timeline"
	"strings"
)

// seedSpaces is fixture-only. Production never creates personal topics or assigns
// mail without a human action or an explicitly enabled classification provider.
func seedSpaces(ctx context.Context, store *identity.Store, a *accounts.Service, syncer *mailsync.Service, box *outbox.Service, owner string) error {
	sources := timeline.New(store, a, syncer, box)
	groups := collections.New(store.Database(), sources)
	existing, err := groups.List(ctx, owner, "space")
	if err != nil || len(existing) > 0 {
		return err
	}
	website, err := groups.Create(ctx, owner, "space", "Website", "Website design, brand assets, interface feedback and the launch website.", "lilac")
	if err != nil {
		return err
	}
	launch, err := groups.Create(ctx, owner, "space", "Launch", "Release planning, announcements, integration handoffs and launch readiness.", "blue")
	if err != nil {
		return err
	}
	studio, err := groups.Create(ctx, owner, "space", "Studio", "Team updates, coffee, links and everyday studio conversations.", "green")
	if err != nil {
		return err
	}
	label, err := groups.Create(ctx, owner, "label", "Follow up", "", "amber")
	if err != nil {
		return err
	}
	records, err := sources.List(ctx, owner, "")
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Preview.Encrypted || record.Own {
			continue
		}
		subject := strings.ToLower(record.Preview.Subject)
		var ids []string
		switch {
		case strings.Contains(subject, "good stuff") || strings.Contains(subject, "brand assets"):
			ids = []string{website.ID}
		case strings.Contains(subject, "launch") || strings.Contains(subject, "api handoff"):
			ids = []string{launch.ID}
		default:
			ids = []string{studio.ID}
		}
		// Demonstrate one message in two Spaces without merging its original thread.
		if strings.Contains(subject, "api handoff") {
			ids = append(ids, website.ID)
		}
		for _, id := range ids {
			if err = groups.Assign(ctx, owner, "space", id, record.Ref, true); err != nil {
				return err
			}
		}
		if strings.Contains(record.Preview.Text, "warmer background") {
			if err = groups.Assign(ctx, owner, "label", label.ID, record.Ref, true); err != nil {
				return err
			}
		}
	}
	return nil
}
