package cache

import (
	"fmt"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/persistence"
)

// RestoreSnapshot loads a fully decoded snapshot into a fresh store at startup.
// It preserves saved LRU order and deadlines without logging or choosing evictions.
func (s *Store) RestoreSnapshot(snapshot persistence.Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if snapshot.Header.MaxBytes != s.maxBytes {
		return fmt.Errorf("restore snapshot: %w", persistence.ErrMemoryBudgetMismatch)
	}
	if len(s.entries) != 0 || s.lru.Len() != 0 || s.usedBytes != 0 {
		return fmt.Errorf("%w: snapshot restore requires an empty cache", persistence.ErrInvalidSnapshot)
	}
	now := time.Now()
	for _, saved := range snapshot.Entries {
		entry := Entry{Value: saved.Value}
		if saved.ExpiresAtUnixNano != nil {
			entry.ExpiresAt = time.Unix(0, *saved.ExpiresAtUnixNano).UTC()
		}
		if entry.IsExpired(now) {
			continue
		}
		s.applySetLocked(saved.Key, entry, calculateEntrySize(saved.Key, entry.Value), nil)
	}
	return nil
}
