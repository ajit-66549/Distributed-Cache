package persistence

import (
	"bufio"
	"fmt"
	"io"
)

// Replay reads a fixed prefix of the validated log and applies its records in
// order. Use it during startup before serving requests. The callback must apply
// records without logging them again; discard the recovering cache on error.
func (aof *AOF) Replay(apply func(Record) error) error {
	return aof.ReplayAfter(0, apply)
}

// ReplayAfter validates every record but applies only sequences newer than the
// restored checkpoint. The caller must restore the matching snapshot first.
func (aof *AOF) ReplayAfter(checkpoint uint64, apply func(Record) error) error {
	if apply == nil {
		return fmt.Errorf("replay AOF: an apply callback is required")
	}
	reader, header, expectedLast, err := aof.replaySource()
	if err != nil {
		return err
	}
	if checkpoint < header.BaseSequence || checkpoint > expectedLast {
		return fmt.Errorf("replay AOF %q: %w: checkpoint %d outside log range %d..%d", aof.path, ErrInvalidSequence, checkpoint, header.BaseSequence, expectedLast)
	}
	lastSequence := header.BaseSequence
	for {
		offset := reader.offset
		record, err := DecodeRecord(reader)
		if err == io.EOF {
			if lastSequence != expectedLast {
				return fmt.Errorf("replay AOF %q: %w: ended at %d, expected %d", aof.path, ErrInvalidSequence, lastSequence, expectedLast)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("replay AOF %q at offset %d: %w", aof.path, offset, err)
		}
		if err := record.ValidateAfter(lastSequence); err != nil {
			return fmt.Errorf("replay AOF %q at offset %d: %w", aof.path, offset, err)
		}
		if record.Sequence > checkpoint {
			if err := apply(record); err != nil {
				return fmt.Errorf("apply AOF %q at offset %d, sequence %d: %w", aof.path, offset, record.Sequence, err)
			}
		}
		lastSequence = record.Sequence
	}
}

// replaySource captures a stable log boundary. Positional reads leave the
// append handle's offset alone. Release mu before callbacks can lock a cache;
// live mutations take the cache lock before taking the AOF lock.
func (aof *AOF) replaySource() (*offsetReader, Header, uint64, error) {
	aof.mu.Lock()
	defer aof.mu.Unlock()
	if err := aof.stateError(); err != nil {
		return nil, Header{}, 0, err
	}
	info, err := aof.file.Stat()
	if err != nil {
		return nil, Header{}, 0, fmt.Errorf("inspect AOF %q for replay: %w", aof.path, err)
	}
	if info.Size() < HeaderSize {
		return nil, Header{}, 0, fmt.Errorf("replay AOF %q: %w: incomplete file header", aof.path, ErrInvalidHeader)
	}
	section := io.NewSectionReader(aof.file, HeaderSize, info.Size()-HeaderSize)
	reader := &offsetReader{reader: bufio.NewReader(section), offset: HeaderSize}
	return reader, aof.header, aof.lastSequence, nil
}
