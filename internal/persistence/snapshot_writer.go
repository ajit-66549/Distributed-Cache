package persistence

import (
	"fmt"
	"path/filepath"
)

// SaveSnapshot publishes the cache state at the current durable sequence. The
// caller must hold the cache lock from capture until this returns. Publication
// is serialized with appends and Close, and does not rotate or truncate the AOF.
func (aof *AOF) SaveSnapshot(snapshot Snapshot) error {
	aof.mu.Lock()
	defer aof.mu.Unlock()
	if err := aof.stateError(); err != nil {
		return err
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	if snapshot.Header.MaxBytes != aof.header.MaxBytes {
		return fmt.Errorf("save snapshot for AOF %q: %w", aof.path, ErrMemoryBudgetMismatch)
	}
	if snapshot.Header.Checkpoint != aof.lastSequence {
		return fmt.Errorf("save snapshot for AOF %q: %w: checkpoint %d, current sequence %d", aof.path, ErrInvalidSequence, snapshot.Header.Checkpoint, aof.lastSequence)
	}
	if err := writeSnapshotFile(filepath.Dir(aof.path), snapshot); err != nil {
		return aof.fail(err)
	}
	return nil
}
