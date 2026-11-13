package persistence

import "fmt"

// validateSnapshotHistory checks coverage before any incomplete tail is removed.
func validateSnapshotHistory(header Header, lastSequence uint64, snapshot *Snapshot) error {
	if snapshot == nil {
		if header.BaseSequence != 0 {
			return fmt.Errorf("%w: base sequence %d requires a snapshot", ErrRecoveryRequired, header.BaseSequence)
		}
		return nil
	}
	if snapshot.Header.MaxBytes != header.MaxBytes {
		return fmt.Errorf("%w: snapshot and AOF budgets differ", ErrMemoryBudgetMismatch)
	}
	checkpoint := snapshot.Header.Checkpoint
	if header.BaseSequence > checkpoint {
		return fmt.Errorf("%w: AOF base %d exceeds snapshot checkpoint %d", ErrRecoveryRequired, header.BaseSequence, checkpoint)
	}
	if lastSequence < checkpoint {
		return fmt.Errorf("%w: %w: AOF ends at %d before snapshot checkpoint %d", ErrRecoveryRequired, ErrInvalidSequence, lastSequence, checkpoint)
	}
	return nil
}
