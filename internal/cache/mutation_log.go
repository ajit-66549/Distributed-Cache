package cache

import "github.com/ajit-66549/distributed-cache/internal/persistence"

// MutationLog describes the persistence operations required by the cache.
// The implementation owns record encoding, file writes, and synchronization.
type MutationLog interface {
	LastSequence() uint64
	Append(record persistence.Record) error
}

// appendSetLocked builds and submits the SET batch before memory is changed.
// The caller must hold s.mu through planning, logging, and applying the batch.
func (s *Store) appendSetLocked(key string, entry Entry, victims []string) error {
	if s.mutationLog == nil {
		return nil
	}
	expiration, err := entry.expirationUnixNano()
	if err != nil {
		return err
	}
	mutations := buildSetMutations(key, entry.Value, expiration, victims)
	return s.appendMutationsLocked(mutations)
}

// appendMutationsLocked submits one prepared batch to the log writer.
// The caller must hold s.mu through planning, logging, and applying the batch.
func (s *Store) appendMutationsLocked(mutations []persistence.Mutation) error {
	if s.mutationLog == nil {
		return nil
	}

	lastSequence := s.mutationLog.LastSequence()
	record := persistence.Record{
		Sequence:  lastSequence + 1,
		Mutations: mutations,
	}

	if err := record.ValidateAfter(lastSequence); err != nil {
		return err
	}

	return s.mutationLog.Append(record)
}
