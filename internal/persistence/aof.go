package persistence

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	aofFilename      = "append.aof"
	snapshotFilename = "snapshot.bin"
	lockFilename     = "LOCK"
)

// AOF owns an exclusively locked append-only log. Append, Sync, and Close are
// serialized. Cache mutation ordering must also be coordinated by the caller.
// An AOF must be opened with OpenAOF and must not be copied after first use.
type AOF struct {
	mu           sync.Mutex
	file         *os.File
	lock         *os.File
	path         string
	header       Header
	lastSequence uint64
	failure      error
	closed       bool
	closeErr     error
}

// OpenAOF creates or validates append.aof and holds the data directory's lock
// until Close. It never truncates existing files or replays data into a cache.
// Incomplete tails and snapshot-backed logs require the later recovery layer.
func OpenAOF(dataDir string, maxBytes int64) (aof *AOF, err error) {
	if dataDir == "" {
		return nil, fmt.Errorf("open AOF: data directory must not be empty")
	}
	header := Header{Version: FormatVersion, MaxBytes: maxBytes}
	encodedHeader, err := EncodeHeader(header)
	if err != nil {
		return nil, fmt.Errorf("open AOF: %w", err)
	}
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	if err := makeDataDirectory(dataDir); err != nil {
		return nil, err
	}
	lock, err := lockDataDirectory(dataDir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, closeFile(lock, "release data directory lock"))
		}
	}()

	// Snapshot validation belongs to recovery. Refuse to create a fresh log
	// beside a snapshot or append to checkpointed data without that validation.
	snapshotPath := filepath.Join(dataDir, snapshotFilename)
	if _, err := os.Lstat(snapshotPath); err == nil {
		return nil, fmt.Errorf("open AOF: %w: snapshot %q must be loaded first", ErrRecoveryRequired, snapshotPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect snapshot %q: %w", snapshotPath, err)
	}

	path := filepath.Join(dataDir, aofFilename)
	file, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0)
	created := false
	if errors.Is(err, os.ErrNotExist) {
		file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR|os.O_APPEND, 0600)
		created = err == nil
	}
	if err != nil {
		return nil, fmt.Errorf("open AOF %q: %w", path, err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, closeFile(file, "close AOF after open failure"))
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect AOF %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("open AOF %q: expected a regular file", path)
	}

	var lastSequence uint64
	if created {
		if err := writeFull(file, encodedHeader); err != nil {
			return nil, fmt.Errorf("write AOF header %q: %w", path, err)
		}
	} else {
		header, lastSequence, err = scanAOF(file, maxBytes)
		if err != nil {
			return nil, err
		}
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("sync AOF %q during open: %w", path, err)
	}
	if err := syncDirectory(dataDir); err != nil {
		return nil, err
	}

	return &AOF{
		file:         file,
		lock:         lock,
		path:         path,
		header:       header,
		lastSequence: lastSequence,
	}, nil
}

// Header returns the immutable metadata read or written during OpenAOF.
func (aof *AOF) Header() Header {
	return aof.header
}

// LastSequence is the last sequence confirmed by validation or a successful
// append and sync. An append that fails may still leave a record on disk.
func (aof *AOF) LastSequence() uint64 {
	aof.mu.Lock()
	defer aof.mu.Unlock()
	return aof.lastSequence
}

// Append validates and writes one record, then syncs it before returning nil.
// Validation failures leave the log usable. Any write or sync failure makes
// further appends fail until the log is closed and recovered.
func (aof *AOF) Append(record Record) error {
	aof.mu.Lock()
	defer aof.mu.Unlock()
	if err := aof.stateError(); err != nil {
		return err
	}
	if err := record.ValidateAfter(aof.lastSequence); err != nil {
		return fmt.Errorf("append AOF %q: %w", aof.path, err)
	}
	frame, err := EncodeRecord(record)
	if err != nil {
		return fmt.Errorf("encode AOF record %q: %w", aof.path, err)
	}
	if err := writeFull(aof.file, frame); err != nil {
		return aof.fail(fmt.Errorf("append AOF %q at sequence %d: %w", aof.path, record.Sequence, err))
	}
	if err := aof.file.Sync(); err != nil {
		return aof.fail(fmt.Errorf("sync AOF %q at sequence %d: %w", aof.path, record.Sequence, err))
	}
	aof.lastSequence = record.Sequence
	return nil
}

// Sync explicitly flushes a healthy log. Append already syncs every record.
// A successful Sync cannot clear a previous failure's uncertain outcome.
func (aof *AOF) Sync() error {
	aof.mu.Lock()
	defer aof.mu.Unlock()
	if err := aof.stateError(); err != nil {
		return err
	}
	if err := aof.file.Sync(); err != nil {
		return aof.fail(fmt.Errorf("sync AOF %q: %w", aof.path, err))
	}
	return nil
}

// Close stops appends, syncs and closes the log, then releases ownership. It
// attempts every cleanup step even after a failure and preserves all errors.
// Repeated calls return the same result. Close does not clear a prior failure.
func (aof *AOF) Close() error {
	aof.mu.Lock()
	defer aof.mu.Unlock()
	if aof.closed {
		return aof.closeErr
	}
	aof.closed = true
	var syncErr error
	if aof.file != nil {
		if err := aof.file.Sync(); err != nil {
			syncErr = fmt.Errorf("sync AOF %q during close: %w", aof.path, err)
		}
	}
	aof.closeErr = errors.Join(
		aof.failure,
		syncErr,
		closeFile(aof.file, "close AOF"),
		closeFile(aof.lock, "release data directory lock"),
	)
	return aof.closeErr
}

// stateError and fail are called only while holding mu.
func (aof *AOF) stateError() error {
	if aof.closed || aof.file == nil {
		return fmt.Errorf("AOF %q: %w", aof.path, ErrAOFClosed)
	}
	if aof.failure != nil {
		return aof.failure
	}
	return nil
}

func (aof *AOF) fail(err error) error {
	aof.failure = fmt.Errorf("%w: %w", ErrAOFFailed, err)
	return aof.failure
}

// scanAOF checks existing bytes before appends are allowed. It does not repair
// damaged tails or apply cache mutations; those are recovery responsibilities.
func scanAOF(file *os.File, maxBytes int64) (Header, uint64, error) {
	reader := &offsetReader{reader: bufio.NewReader(file)}
	header, err := DecodeHeader(reader)
	if err != nil {
		return Header{}, 0, fmt.Errorf("read AOF %q at offset 0: %w", file.Name(), err)
	}
	if header.MaxBytes != maxBytes {
		return Header{}, 0, fmt.Errorf("AOF %q: %w: stored %d, configured %d", file.Name(), ErrMemoryBudgetMismatch, header.MaxBytes, maxBytes)
	}
	if header.BaseSequence != 0 {
		return Header{}, 0, fmt.Errorf("AOF %q: %w: base sequence %d requires a validated snapshot", file.Name(), ErrRecoveryRequired, header.BaseSequence)
	}
	lastSequence := header.BaseSequence
	for {
		offset := reader.offset
		record, err := DecodeRecord(reader)
		if err == io.EOF {
			return header, lastSequence, nil
		}
		if err != nil {
			if errors.Is(err, ErrTruncatedRecord) {
				err = fmt.Errorf("%w: %w", ErrRecoveryRequired, err)
			}
			return Header{}, 0, fmt.Errorf("read AOF %q at offset %d: %w", file.Name(), offset, err)
		}
		if err := record.ValidateAfter(lastSequence); err != nil {
			return Header{}, 0, fmt.Errorf("read AOF %q at offset %d: %w", file.Name(), offset, err)
		}
		lastSequence = record.Sequence
	}
}

type offsetReader struct {
	reader io.Reader
	offset int64
}

func (reader *offsetReader) Read(data []byte) (int, error) {
	count, err := reader.reader.Read(data)
	reader.offset += int64(count)
	return count, err
}

func writeFull(writer io.Writer, data []byte) error {
	count, err := writer.Write(data)
	if err != nil {
		return fmt.Errorf("wrote %d of %d bytes: %w", count, len(data), err)
	}
	if count != len(data) {
		return fmt.Errorf("wrote %d of %d bytes: %w", count, len(data), io.ErrShortWrite)
	}
	return nil
}

func closeFile(file *os.File, operation string) error {
	if file == nil {
		return nil
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("%s %q: %w", operation, file.Name(), err)
	}
	return nil
}
