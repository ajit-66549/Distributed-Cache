package persistence

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// makeDataDirectory synchronizes each directory and its parent, including
// existing paths left behind by an interrupted initialization attempt.
func makeDataDirectory(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("data directory %q is not a directory", path)
		}
		return syncDirectoryEntry(path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect data directory %q: %w", path, err)
	}
	parent := filepath.Dir(path)
	if parent == path {
		return fmt.Errorf("create data directory %q: %w", path, err)
	}
	if err := makeDataDirectory(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create data directory %q: %w", path, err)
	}
	return syncDirectoryEntry(path)
}

func syncDirectoryEntry(path string) error {
	if err := syncDirectory(path); err != nil {
		return err
	}
	if parent := filepath.Dir(path); parent != path {
		return syncDirectory(parent)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory %q for sync: %w", path, err)
	}
	var syncErr error
	if err := directory.Sync(); err != nil {
		syncErr = fmt.Errorf("sync directory %q: %w", path, err)
	}
	return errors.Join(syncErr, closeFile(directory, "close directory after sync"))
}
