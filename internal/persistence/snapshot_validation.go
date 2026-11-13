package persistence

import (
	"fmt"
	"unicode/utf8"
)

// Validate checks metadata, entries, uniqueness, and budget without doing I/O.
// Expiration is checked during capture and restore, not during format validation.
func (snapshot Snapshot) Validate() error {
	if err := snapshot.Header.validate(); err != nil {
		return err
	}
	if snapshot.Header.EntryCount != uint64(len(snapshot.Entries)) {
		return fmt.Errorf("%w: entry count does not match entries", ErrInvalidSnapshot)
	}
	seen := make(map[string]bool, len(snapshot.Entries))
	var usedBytes int64
	for index, entry := range snapshot.Entries {
		if err := entry.validate(); err != nil {
			return fmt.Errorf("snapshot entry %d: %w", index, err)
		}
		if seen[entry.Key] {
			return fmt.Errorf("%w: duplicate key %q", ErrInvalidSnapshot, entry.Key)
		}
		seen[entry.Key] = true
		size := int64(len(entry.Key)) + int64(len(entry.Value))
		if size > snapshot.Header.MaxBytes || usedBytes > snapshot.Header.MaxBytes-size {
			return fmt.Errorf("%w: entries exceed the memory budget", ErrInvalidSnapshot)
		}
		usedBytes += size
	}
	return nil
}

func (header SnapshotHeader) validate() error {
	if header.Version != SnapshotFormatVersion {
		return fmt.Errorf("%w: %w: snapshot version %d", ErrInvalidSnapshot, ErrUnsupportedVersion, header.Version)
	}
	if header.MaxBytes <= 0 {
		return fmt.Errorf("%w: memory budget must be positive", ErrInvalidSnapshot)
	}
	if header.Checkpoint == 0 && header.EntryCount != 0 {
		return fmt.Errorf("%w: checkpoint zero requires an empty snapshot", ErrInvalidSnapshot)
	}
	if header.EntryCount > uint64(header.MaxBytes) {
		return fmt.Errorf("%w: entry count exceeds the memory budget", ErrInvalidSnapshot)
	}
	return nil
}

func (entry SnapshotEntry) validate() error {
	if len(entry.Key) == 0 || len(entry.Key) > maxKeyBytes || !utf8.ValidString(entry.Key) {
		return fmt.Errorf("%w: key must contain 1 to %d valid UTF-8 bytes", ErrInvalidSnapshot, maxKeyBytes)
	}
	if len(entry.Value) > maxValueBytes || !utf8.ValidString(entry.Value) {
		return fmt.Errorf("%w: value must contain at most %d valid UTF-8 bytes", ErrInvalidSnapshot, maxValueBytes)
	}
	return nil
}
