// SPDX-License-Identifier: AGPL-3.0-only
package mailsync

import (
	"context"
	"time"
)

type syncRequest struct {
	owner   string
	account string
}

// Schedule is bounded and nonblocking. Queue overflow is retried by polling.
func (s *Service) Schedule(owner, account string) {
	select {
	case s.queue <- syncRequest{owner, account}:
	default:
	}
}

// Run uses polling as a portable fallback for servers without IDLE. Each pass
// advances through eight accounts; a persistent cursor prevents starvation.
// It never creates an index or calls AI, and credentials stay in WithIMAP.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	cursor := ""
	poll := func() {
		rows, err := s.db.QueryContext(ctx, `SELECT owner_id,accounts.id FROM accounts JOIN mail_connections ON account_id=accounts.id WHERE accounts.id>? ORDER BY accounts.id LIMIT 8`, cursor)
		if err != nil {
			return
		}
		batch := []syncRequest{}
		for rows.Next() {
			var item syncRequest
			if err = rows.Scan(&item.owner, &item.account); err != nil {
				rows.Close()
				return
			}
			batch = append(batch, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return
		}
		if len(batch) < 8 {
			cursor = ""
		} else {
			cursor = batch[len(batch)-1].account
		}
		for _, item := range batch {
			if ctx.Err() != nil {
				return
			}
			_ = s.Sync(ctx, item.owner, item.account)
		}
	}
	poll()
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-s.queue:
			if ctx.Err() != nil {
				return
			}
			_ = s.Sync(ctx, item.owner, item.account)
		case <-ticker.C:
			poll()
		}
	}
}
