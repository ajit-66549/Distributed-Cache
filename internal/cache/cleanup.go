package cache

import (
	"context"
	"time"
)

func StartExpirationCleanup(ctx context.Context, store *Store, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				store.DeleteExpired()

			case <-ctx.Done():
				return
			}
		}
	}()
}
