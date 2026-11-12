package persistence

// Snapshot format constants are independent of the AOF format version.
// The byte layout and validation rules are defined in docs/design.md, section 9.7.
const (
	SnapshotFormatVersion uint16 = 1
	SnapshotHeaderSize           = 38 // Magic, version, checkpoint, budget, count, CRC.
	SnapshotFooterSize           = 4  // CRC-32 of the header and all entry frames.

	// Allows JSON escaping of keys and values within the existing cache limits.
	MaxSnapshotEntryPayloadSize = 8 * 1024 * 1024

	snapshotMagic           = "CACHESNP"
	snapshotEntryHeaderSize = 8 // Payload length and its CRC-32.
)

// SnapshotHeader describes the cache state represented by one snapshot.
type SnapshotHeader struct {
	Version    uint16
	Checkpoint uint64 // All AOF records through this sequence are reflected.
	MaxBytes   int64  // The same key/value byte budget recorded in the AOF.
	EntryCount uint64 // Must match the number of encoded entries.
}

// SnapshotEntry is one saved key/value pair, without an operation or sequence.
// A nil deadline means no expiration; a timestamp preserves the original deadline.
type SnapshotEntry struct {
	Key               string `json:"key"`
	Value             string `json:"value"`
	ExpiresAtUnixNano *int64 `json:"expires_at_unix_nano"`
}

// Snapshot contains the live cache state captured at Header.Checkpoint.
// Entries are unique by key and ordered from least to most recently used.
// The whole file must validate before any entries are restored into the cache.
type Snapshot struct {
	Header  SnapshotHeader
	Entries []SnapshotEntry
}
