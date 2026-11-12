package persistence

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// writeSnapshotFile publishes a validated snapshot within an already locked
// data directory. The active AOF is never changed by snapshot publication.
func writeSnapshotFile(dataDir string, snapshot Snapshot) (err error) {
	file, err := os.CreateTemp(dataDir, ".snapshot-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary snapshot in %q: %w", dataDir, err)
	}
	temporaryPath := file.Name()
	defer func() {
		err = errors.Join(err, closeFile(file, "close temporary snapshot"))
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temporary snapshot %q: %w", temporaryPath, removeErr))
		}
	}()
	buffer := bufio.NewWriter(file)
	if err := encodeSnapshot(buffer, snapshot); err != nil {
		return fmt.Errorf("encode snapshot %q: %w", temporaryPath, err)
	}
	if err := buffer.Flush(); err != nil {
		return fmt.Errorf("flush snapshot %q: %w", temporaryPath, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync snapshot %q: %w", temporaryPath, err)
	}
	closeErr := closeFile(file, "close snapshot before publication")
	file = nil
	if closeErr != nil {
		return closeErr
	}
	path := filepath.Join(dataDir, snapshotFilename)
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish snapshot %q: %w", path, err)
	}
	return syncDirectory(dataDir)
}
