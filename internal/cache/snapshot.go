package cache

import (
	"errors"

	"github.com/ajit-66549/distributed-cache/internal/persistence"
)

// snapshotWriter is the additional capability required to publish snapshots.
// Keeping it separate lets mutation-only log implementations remain unchanged.
type snapshotWriter interface {
	SaveSnapshot(snapshot persistence.Snapshot) error
}

// SaveSnapshot pauses cache operations through capture and durable publication.
// The attached log owns encoding, file creation, synchronization, and rename.
func (s *Store) SaveSnapshot() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	writer, ok := s.mutationLog.(snapshotWriter)
	if !ok {
		return errors.New("attached mutation log does not support snapshots")
	}
	snapshot, err := s.captureSnapshotLocked()
	if err != nil {
		return err
	}
	return writer.SaveSnapshot(snapshot)
}
