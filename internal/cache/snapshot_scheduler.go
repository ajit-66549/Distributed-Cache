package cache

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// StartSnapshotScheduler starts one worker whose first save follows one interval.
// Cancel ctx and receive from the returned channel before closing persistence.
// The channel receives one result (nil on cancellation), then closes. An active
// save finishes before the worker stops; a save error stops further scheduling.
func StartSnapshotScheduler(ctx context.Context, store *Store, interval time.Duration) (<-chan error, error) {
	if ctx == nil {
		return nil, errors.New("snapshot scheduler requires a context")
	}
	if store == nil {
		return nil, errors.New("snapshot scheduler requires a store")
	}
	if interval <= 0 {
		return nil, errors.New("snapshot interval must be positive")
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- runSnapshotScheduler(ctx, store, interval)
	}()
	return done, nil
}

func runSnapshotScheduler(ctx context.Context, store *Store, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Cancellation can be ready at the same time as a tick.
			if ctx.Err() != nil {
				return nil
			}
			// Save synchronously so slow saves cannot overlap within this worker.
			if err := store.SaveSnapshot(); err != nil {
				log.Printf("periodic snapshot failed: %v", err)
				return fmt.Errorf("save periodic snapshot: %w", err)
			}
			log.Println("periodic snapshot saved and AOF rotated")
		}
	}
}
