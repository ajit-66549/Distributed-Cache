package persistence

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"unicode/utf8"
)

// DecodeSnapshot returns data only after the entire file validates. It bounds
// each allocation and checks the configured budget before reading entries.
func DecodeSnapshot(reader io.Reader, maxBytes int64) (Snapshot, error) {
	if reader == nil || maxBytes <= 0 {
		return Snapshot{}, fmt.Errorf("%w: a reader and positive memory budget are required", ErrInvalidSnapshot)
	}
	tracked := &offsetReader{reader: reader}
	checksum := crc32.NewIEEE()
	input := io.TeeReader(tracked, checksum)
	header, err := decodeSnapshotHeader(input)
	if err != nil {
		return Snapshot{}, err
	}
	if header.MaxBytes != maxBytes {
		return Snapshot{}, fmt.Errorf("%w: %w: stored %d, configured %d", ErrInvalidSnapshot, ErrMemoryBudgetMismatch, header.MaxBytes, maxBytes)
	}
	snapshot := Snapshot{Header: header}
	seen := make(map[string]bool)
	var usedBytes int64
	for index := uint64(0); index < header.EntryCount; index++ {
		offset := tracked.offset
		entry, err := decodeSnapshotEntry(input)
		if err != nil {
			return Snapshot{}, fmt.Errorf("snapshot entry %d at offset %d: %w", index, offset, err)
		}
		if seen[entry.Key] {
			return Snapshot{}, fmt.Errorf("%w: duplicate key %q at entry %d", ErrInvalidSnapshot, entry.Key, index)
		}
		size := int64(len(entry.Key)) + int64(len(entry.Value))
		if size > maxBytes || usedBytes > maxBytes-size {
			return Snapshot{}, fmt.Errorf("%w: entries exceed the memory budget", ErrInvalidSnapshot)
		}
		usedBytes += size
		seen[entry.Key] = true
		snapshot.Entries = append(snapshot.Entries, entry)
	}
	var footer [SnapshotFooterSize]byte
	if _, err := io.ReadFull(tracked, footer[:]); err != nil {
		return Snapshot{}, fmt.Errorf("%w: read file checksum: %w", ErrInvalidSnapshot, err)
	}
	if binary.BigEndian.Uint32(footer[:]) != checksum.Sum32() {
		return Snapshot{}, fmt.Errorf("%w: %w: whole snapshot file", ErrInvalidSnapshot, ErrChecksumMismatch)
	}
	var extra [1]byte
	if count, err := io.ReadFull(tracked, extra[:]); count != 0 || err != io.EOF {
		if err != nil {
			return Snapshot{}, fmt.Errorf("%w: read snapshot end: %w", ErrInvalidSnapshot, err)
		}
		return Snapshot{}, fmt.Errorf("%w: trailing bytes after file checksum", ErrInvalidSnapshot)
	}
	return snapshot, nil
}

func decodeSnapshotHeader(reader io.Reader) (SnapshotHeader, error) {
	var data [SnapshotHeaderSize]byte
	if _, err := io.ReadFull(reader, data[:]); err != nil {
		return SnapshotHeader{}, fmt.Errorf("%w: read snapshot header: %w", ErrInvalidSnapshot, err)
	}
	if binary.BigEndian.Uint32(data[34:38]) != crc32.ChecksumIEEE(data[:34]) {
		return SnapshotHeader{}, fmt.Errorf("%w: %w: snapshot header", ErrInvalidSnapshot, ErrChecksumMismatch)
	}
	if string(data[:8]) != snapshotMagic {
		return SnapshotHeader{}, fmt.Errorf("%w: unexpected snapshot magic", ErrInvalidSnapshot)
	}
	maxBytes := binary.BigEndian.Uint64(data[18:26])
	if maxBytes > math.MaxInt64 {
		return SnapshotHeader{}, fmt.Errorf("%w: memory budget exceeds signed 64-bit range", ErrInvalidSnapshot)
	}
	header := SnapshotHeader{
		Version:    binary.BigEndian.Uint16(data[8:10]),
		Checkpoint: binary.BigEndian.Uint64(data[10:18]),
		MaxBytes:   int64(maxBytes),
		EntryCount: binary.BigEndian.Uint64(data[26:34]),
	}
	if err := header.validate(); err != nil {
		return SnapshotHeader{}, err
	}
	return header, nil
}

func decodeSnapshotEntry(reader io.Reader) (SnapshotEntry, error) {
	var header [snapshotEntryHeaderSize]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return SnapshotEntry{}, fmt.Errorf("%w: read entry header: %w", ErrInvalidSnapshot, err)
	}
	if binary.BigEndian.Uint32(header[4:8]) != crc32.ChecksumIEEE(header[:4]) {
		return SnapshotEntry{}, fmt.Errorf("%w: %w: entry length", ErrInvalidSnapshot, ErrChecksumMismatch)
	}
	length := binary.BigEndian.Uint32(header[:4])
	if length == 0 || length > MaxSnapshotEntryPayloadSize {
		return SnapshotEntry{}, fmt.Errorf("%w: invalid entry payload length %d", ErrInvalidSnapshot, length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return SnapshotEntry{}, fmt.Errorf("%w: read entry payload: %w", ErrInvalidSnapshot, err)
	}
	var trailer [4]byte
	if _, err := io.ReadFull(reader, trailer[:]); err != nil {
		return SnapshotEntry{}, fmt.Errorf("%w: read entry checksum: %w", ErrInvalidSnapshot, err)
	}
	checksum := crc32.Update(crc32.ChecksumIEEE(header[:4]), crc32.IEEETable, payload)
	if binary.BigEndian.Uint32(trailer[:]) != checksum {
		return SnapshotEntry{}, fmt.Errorf("%w: %w: entry payload", ErrInvalidSnapshot, ErrChecksumMismatch)
	}
	if !utf8.Valid(payload) {
		return SnapshotEntry{}, fmt.Errorf("%w: entry payload is not valid UTF-8", ErrInvalidSnapshot)
	}
	fields, err := decodeObject(payload, "key", "value", "expires_at_unix_nano")
	if err != nil {
		return SnapshotEntry{}, fmt.Errorf("%w: %w", ErrInvalidSnapshot, err)
	}
	if len(fields) != 3 {
		return SnapshotEntry{}, fmt.Errorf("%w: all entry fields are required", ErrInvalidSnapshot)
	}
	for _, name := range []string{"key", "value"} {
		if bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
			return SnapshotEntry{}, fmt.Errorf("%w: %s must be a string", ErrInvalidSnapshot, name)
		}
	}
	var entry SnapshotEntry
	if err := json.Unmarshal(payload, &entry); err != nil {
		return SnapshotEntry{}, fmt.Errorf("%w: decode entry: %w", ErrInvalidSnapshot, err)
	}
	if err := entry.validate(); err != nil {
		return SnapshotEntry{}, err
	}
	return entry, nil
}
