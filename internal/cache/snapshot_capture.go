package cache

import (
	"fmt"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/persistence"
)

// captureSnapshotLocked copies live entries in least-to-most-recent LRU order.
// The caller must hold s.mu, have an attached log, and keep the lock until the
// snapshot publication and AOF rotation finish. Capture does not delete entries
// or append log records.
func (s *Store) captureSnapshotLocked() (persistence.Snapshot, error) {
	checkpoint := s.mutationLog.LastSequence()
	now := time.Now()
	entries := make([]persistence.SnapshotEntry, 0, len(s.entries))
	for element := s.lru.Back(); element != nil; element = element.Prev() {
		item := element.Value.(*lruItem)
		if item.Entry.IsExpired(now) {
			continue
		}
		expiration, err := item.Entry.expirationUnixNano()
		if err != nil {
			return persistence.Snapshot{}, fmt.Errorf("capture snapshot key %q: %w", item.Key, err)
		}
		entries = append(entries, persistence.SnapshotEntry{
			Key:               item.Key,
			Value:             item.Entry.Value,
			ExpiresAtUnixNano: expiration,
		})
	}
	return persistence.Snapshot{
		Header: persistence.SnapshotHeader{
			Version:    persistence.SnapshotFormatVersion,
			Checkpoint: checkpoint,
			MaxBytes:   s.maxBytes,
			EntryCount: uint64(len(entries)),
		},
		Entries: entries,
	}, nil
}
