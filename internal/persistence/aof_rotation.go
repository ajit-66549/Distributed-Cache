package persistence

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// rotateLocked retires records covered by the just-published durable snapshot.
// The caller must hold both cache and AOF locks until rotation finishes, and
// mark the AOF failed on any error before allowing another mutation.
func (aof *AOF) rotateLocked() (err error) {
	header := aof.header
	header.BaseSequence = aof.lastSequence
	dataDir := filepath.Dir(aof.path)
	replacement, err := prepareAOFReplacement(dataDir, header)
	if err != nil {
		return err
	}
	temporaryPath := replacement.Name()
	defer func() {
		err = errors.Join(err, closeFile(replacement, "close unpublished AOF replacement"))
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temporary AOF %q: %w", temporaryPath, removeErr))
		}
	}()
	if err := os.Rename(temporaryPath, aof.path); err != nil {
		return fmt.Errorf("publish replacement AOF %q: %w", aof.path, err)
	}
	if err := syncDirectory(dataDir); err != nil {
		return err
	}
	oldFile := aof.file
	aof.file = replacement
	aof.header = header
	replacement = nil // Ownership has transferred to AOF; deferred cleanup skips it.
	return closeFile(oldFile, "close retired AOF")
}

// prepareAOFReplacement writes, syncs, and closes a header-only temporary log,
// then reopens it for appends before publication. The caller owns the returned
// handle and must either publish it or close it and remove its temporary path.
func prepareAOFReplacement(dataDir string, header Header) (replacement *os.File, err error) {
	encoded, err := EncodeHeader(header)
	if err != nil {
		return nil, fmt.Errorf("encode replacement AOF header: %w", err)
	}
	file, err := os.CreateTemp(dataDir, ".aof-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temporary AOF in %q: %w", dataDir, err)
	}
	temporaryPath := file.Name()
	defer func() {
		err = errors.Join(err, closeFile(file, "close temporary AOF"))
		if err != nil {
			if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove temporary AOF %q: %w", temporaryPath, removeErr))
			}
		}
	}()
	if err := writeFull(file, encoded); err != nil {
		return nil, fmt.Errorf("write replacement AOF %q: %w", temporaryPath, err)
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("sync replacement AOF %q: %w", temporaryPath, err)
	}
	closeErr := closeFile(file, "close replacement AOF before publication")
	file = nil
	if closeErr != nil {
		return nil, closeErr
	}
	replacement, err = os.OpenFile(temporaryPath, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return nil, fmt.Errorf("open replacement AOF %q for appends: %w", temporaryPath, err)
	}
	return replacement, nil
}
