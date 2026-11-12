//go:build darwin || linux

package persistence

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func lockDataDirectory(dataDir string) (*os.File, error) {
	path := filepath.Join(dataDir, lockFilename)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open directory lock %q: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			err = fmt.Errorf("%w: %q", ErrAOFInUse, dataDir)
		} else {
			err = fmt.Errorf("lock data directory %q: %w", dataDir, err)
		}
		return nil, errors.Join(err, closeFile(file, "close unsuccessful directory lock"))
	}
	// Keep this file present: unlinking it could let another process lock a
	// different inode while the current process still owns the original lock.
	return file, nil
}
