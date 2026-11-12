package cache

import (
	"fmt"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/persistence"
)

// ApplyRecoveredRecord restores one record without appending it to the log or
// selecting new eviction victims. Call it only while recovering before serving.
func (s *Store) ApplyRecoveredRecord(record persistence.Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if err := s.validateRecoveryBudgetLocked(record, now); err != nil {
		return err
	}
	for _, mutation := range record.Mutations {
		if mutation.Op == persistence.OpSet {
			entry := entryFromSetMutation(mutation)
			if !entry.IsExpired(now) {
				s.applySetLocked(mutation.Key, entry, calculateEntrySize(mutation.Key, entry.Value), nil)
				continue
			}
		}
		// DELETE and expired SET both remove any older value for this key.
		if element, found := s.entries[mutation.Key]; found {
			s.removeElementLocked(element)
		}
	}
	return nil
}

// validateRecoveryBudgetLocked checks the complete batch before changing memory.
// A valid record is one DELETE or eviction DELETEs followed by a final SET.
func (s *Store) validateRecoveryBudgetLocked(record persistence.Record, now time.Time) error {
	usedBytes := s.usedBytes
	removed := make(map[string]bool, len(record.Mutations))
	for _, mutation := range record.Mutations {
		if !removed[mutation.Key] {
			if element, found := s.entries[mutation.Key]; found {
				usedBytes -= element.Value.(*lruItem).Size
			}
			removed[mutation.Key] = true
		}
		if mutation.Op == persistence.OpSet {
			entry := entryFromSetMutation(mutation)
			if entry.IsExpired(now) {
				continue
			}
			size := calculateEntrySize(mutation.Key, entry.Value)
			if size > s.maxBytes || usedBytes > s.maxBytes-size {
				return fmt.Errorf("%w: recovered sequence %d exceeds the cache memory budget", persistence.ErrInvalidRecord, record.Sequence)
			}
			usedBytes += size
		}
	}
	return nil
}

// entryFromSetMutation reconstructs an entry from a validated SET mutation.
// It preserves the saved deadline; replay handles expiration and insertion.
func entryFromSetMutation(mutation persistence.Mutation) Entry {
	entry := Entry{Value: mutation.Value}
	if mutation.ExpiresAtUnixNano != nil {
		entry.ExpiresAt = time.Unix(0, *mutation.ExpiresAtUnixNano).UTC()
	}
	return entry
}
