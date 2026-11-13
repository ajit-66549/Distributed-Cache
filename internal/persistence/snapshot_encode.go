package persistence

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
)

// encodeSnapshot streams a validated snapshot, buffering only one entry frame.
// The caller must validate the snapshot and keep its contents stable until done.
func encodeSnapshot(writer io.Writer, snapshot Snapshot) error {
	checksum := crc32.NewIEEE()
	output := io.MultiWriter(writer, checksum)
	if err := writeFull(output, encodeSnapshotHeader(snapshot.Header)); err != nil {
		return fmt.Errorf("write snapshot header: %w", err)
	}
	for index, entry := range snapshot.Entries {
		frame, err := encodeSnapshotEntry(entry)
		if err != nil {
			return fmt.Errorf("encode snapshot entry %d: %w", index, err)
		}
		if err := writeFull(output, frame); err != nil {
			return fmt.Errorf("write snapshot entry %d: %w", index, err)
		}
	}
	var footer [SnapshotFooterSize]byte
	binary.BigEndian.PutUint32(footer[:], checksum.Sum32())
	if err := writeFull(writer, footer[:]); err != nil {
		return fmt.Errorf("write snapshot file checksum: %w", err)
	}
	return nil
}

func encodeSnapshotHeader(header SnapshotHeader) []byte {
	data := make([]byte, SnapshotHeaderSize)
	copy(data[:8], snapshotMagic)
	binary.BigEndian.PutUint16(data[8:10], header.Version)
	binary.BigEndian.PutUint64(data[10:18], header.Checkpoint)
	binary.BigEndian.PutUint64(data[18:26], uint64(header.MaxBytes))
	binary.BigEndian.PutUint64(data[26:34], header.EntryCount)
	binary.BigEndian.PutUint32(data[34:38], crc32.ChecksumIEEE(data[:34]))
	return data
}

func encodeSnapshotEntry(entry SnapshotEntry) ([]byte, error) {
	payload, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("%w: encode entry: %w", ErrInvalidSnapshot, err)
	}
	if len(payload) == 0 || len(payload) > MaxSnapshotEntryPayloadSize {
		return nil, fmt.Errorf("%w: entry payload exceeds the format limit", ErrInvalidSnapshot)
	}
	frame := make([]byte, snapshotEntryHeaderSize, snapshotEntryHeaderSize+len(payload)+4)
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	binary.BigEndian.PutUint32(frame[4:8], crc32.ChecksumIEEE(frame[:4]))
	frame = append(frame, payload...)
	checksum := crc32.Update(crc32.ChecksumIEEE(frame[:4]), crc32.IEEETable, payload)
	return binary.BigEndian.AppendUint32(frame, checksum), nil
}
