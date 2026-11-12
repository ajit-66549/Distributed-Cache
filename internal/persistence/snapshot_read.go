package persistence

import (
	"bufio"
	"errors"
	"fmt"
	"os"
)

// readSnapshotFile runs only while the caller owns the data directory lock.
func readSnapshotFile(path string, maxBytes int64) (snapshot *Snapshot, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect snapshot %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: snapshot %q must be a regular file", ErrInvalidSnapshot, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open snapshot %q: %w", path, err)
	}
	defer func() {
		err = errors.Join(err, closeFile(file, "close snapshot after reading"))
	}()
	decoded, err := DecodeSnapshot(bufio.NewReader(file), maxBytes)
	if err != nil {
		return nil, fmt.Errorf("read snapshot %q: %w", path, err)
	}
	return &decoded, nil
}
