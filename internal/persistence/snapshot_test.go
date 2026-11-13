package persistence

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"reflect"
	"testing"
)

// Encoding deliberately invalid structs also supplies checksummed malformed
// fixtures, so decoder rejection cannot rely on a checksum failure alone.
func snapshotFixture(t *testing.T, snapshot Snapshot) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := encodeSnapshot(&buffer, snapshot); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestSnapshotRoundTrip(t *testing.T) {
	deadline := int64(1<<62) + 1
	for _, snapshot := range []Snapshot{
		{Header: SnapshotHeader{Version: SnapshotFormatVersion, MaxBytes: 128}},
		{
			Header: SnapshotHeader{Version: SnapshotFormatVersion, Checkpoint: 1<<53 + 7, MaxBytes: 128, EntryCount: 3},
			Entries: []SnapshotEntry{
				{Key: "session", Value: "abc", ExpiresAtUnixNano: &deadline},
				{Key: "empty", Value: ""},
				{Key: "नमस्ते", Value: "<&>\x00"},
			},
		},
	} {
		data := snapshotFixture(t, snapshot)
		got, err := DecodeSnapshot(bytes.NewReader(data), snapshot.Header.MaxBytes)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, snapshot) {
			t.Fatalf("round trip changed snapshot:\ngot  %+v\nwant %+v", got, snapshot)
		}
		if len(snapshot.Entries) == 0 && len(data) != 42 {
			t.Fatalf("empty snapshot size = %d, want 42", len(data))
		}
	}
}

func TestSnapshotRejectsDamagedFiles(t *testing.T) {
	snapshot := Snapshot{
		Header:  SnapshotHeader{Version: SnapshotFormatVersion, Checkpoint: 4, MaxBytes: 128, EntryCount: 2},
		Entries: []SnapshotEntry{{Key: "name", Value: "Amit"}, {Key: "city", Value: "Pune"}},
	}
	valid := snapshotFixture(t, snapshot)
	for _, test := range []struct {
		name   string
		damage func([]byte) []byte
		want   error
	}{
		{"header checksum", func(data []byte) []byte { data[10] ^= 1; return data }, ErrChecksumMismatch},
		{"entry length checksum", func(data []byte) []byte { data[SnapshotHeaderSize+4] ^= 1; return data }, ErrChecksumMismatch},
		{"entry payload", func(data []byte) []byte { data[bytes.Index(data, []byte("Amit"))] = 'B'; return data }, ErrChecksumMismatch},
		{"file checksum", func(data []byte) []byte { data[len(data)-1] ^= 1; return data }, ErrChecksumMismatch},
		{"incomplete header", func(data []byte) []byte { return data[:SnapshotHeaderSize-1] }, ErrInvalidSnapshot},
		{"incomplete entry header", func(data []byte) []byte { return data[:SnapshotHeaderSize+3] }, ErrInvalidSnapshot},
		{"incomplete payload", func(data []byte) []byte { return data[:SnapshotHeaderSize+snapshotEntryHeaderSize+2] }, ErrInvalidSnapshot},
		{"incomplete footer", func(data []byte) []byte { return data[:len(data)-1] }, ErrInvalidSnapshot},
		{"trailing bytes", func(data []byte) []byte { return append(data, '\n') }, ErrInvalidSnapshot},
		{"reordered valid entry frames", func(data []byte) []byte {
			firstEnd := SnapshotHeaderSize + snapshotEntryHeaderSize + int(binary.BigEndian.Uint32(data[SnapshotHeaderSize:])) + 4
			footerStart := len(data) - SnapshotFooterSize
			reordered := append([]byte(nil), data[:SnapshotHeaderSize]...)
			reordered = append(reordered, data[firstEnd:footerStart]...)
			reordered = append(reordered, data[SnapshotHeaderSize:firstEnd]...)
			return append(reordered, data[footerStart:]...)
		}, ErrChecksumMismatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := DecodeSnapshot(bytes.NewReader(test.damage(bytes.Clone(valid))), 128)
			if !errors.Is(err, test.want) {
				t.Fatalf("DecodeSnapshot() = %v, want %v", err, test.want)
			}
			if !reflect.DeepEqual(got, Snapshot{}) {
				t.Fatal("decoder returned partial state after rejecting a snapshot")
			}
		})
	}
	if _, err := DecodeSnapshot(bytes.NewReader(valid), 256); !errors.Is(err, ErrMemoryBudgetMismatch) {
		t.Fatalf("mismatched budget error = %v", err)
	}
}

func TestSnapshotRejectsInvalidChecksummedState(t *testing.T) {
	for _, test := range []struct {
		name     string
		snapshot Snapshot
		want     error
	}{
		{
			name: "duplicate keys",
			snapshot: Snapshot{
				Header:  SnapshotHeader{Version: SnapshotFormatVersion, Checkpoint: 1, MaxBytes: 128, EntryCount: 2},
				Entries: []SnapshotEntry{{Key: "name", Value: "old"}, {Key: "name", Value: "new"}},
			},
			want: ErrInvalidSnapshot,
		},
		{
			name: "entries exceed budget",
			snapshot: Snapshot{
				Header:  SnapshotHeader{Version: SnapshotFormatVersion, Checkpoint: 1, MaxBytes: 1, EntryCount: 1},
				Entries: []SnapshotEntry{{Key: "name", Value: "Amit"}},
			},
			want: ErrInvalidSnapshot,
		},
		{
			name: "entry count too large",
			snapshot: Snapshot{
				Header:  SnapshotHeader{Version: SnapshotFormatVersion, Checkpoint: 1, MaxBytes: 128, EntryCount: 2},
				Entries: []SnapshotEntry{{Key: "name", Value: "Amit"}},
			},
			want: ErrInvalidSnapshot,
		},
		{
			name: "unsupported version",
			snapshot: Snapshot{
				Header: SnapshotHeader{Version: SnapshotFormatVersion + 1, MaxBytes: 128},
			},
			want: ErrUnsupportedVersion,
		},
		{
			name: "nonempty initial checkpoint",
			snapshot: Snapshot{
				Header:  SnapshotHeader{Version: SnapshotFormatVersion, MaxBytes: 128, EntryCount: 1},
				Entries: []SnapshotEntry{{Key: "name", Value: "Amit"}},
			},
			want: ErrInvalidSnapshot,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeSnapshot(bytes.NewReader(snapshotFixture(t, test.snapshot)), test.snapshot.Header.MaxBytes)
			if !errors.Is(err, test.want) {
				t.Fatalf("DecodeSnapshot() = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSnapshotRejectsMalformedEntryJSON(t *testing.T) {
	for _, payload := range []string{
		`{"key":"a","value":null,"expires_at_unix_nano":null}`,
		`{"key":"a","value":"1"}`,
		`{"key":"a","value":"1","value":"2","expires_at_unix_nano":null}`,
		`{"Key":"a","value":"1","expires_at_unix_nano":null}`,
		`{"key":"a","value":"1","expires_at_unix_nano":9223372036854775808}`,
	} {
		t.Run(payload, func(t *testing.T) {
			header := SnapshotHeader{Version: SnapshotFormatVersion, Checkpoint: 1, MaxBytes: 128, EntryCount: 1}
			frame := binary.BigEndian.AppendUint32(nil, uint32(len(payload)))
			frame = binary.BigEndian.AppendUint32(frame, crc32.ChecksumIEEE(frame))
			frame = append(frame, payload...)
			entryCRC := crc32.Update(crc32.ChecksumIEEE(frame[:4]), crc32.IEEETable, []byte(payload))
			frame = binary.BigEndian.AppendUint32(frame, entryCRC)
			data := append(encodeSnapshotHeader(header), frame...)
			data = binary.BigEndian.AppendUint32(data, crc32.ChecksumIEEE(data))
			if _, err := DecodeSnapshot(bytes.NewReader(data), 128); !errors.Is(err, ErrInvalidSnapshot) {
				t.Fatalf("malformed entry accepted or misclassified: %v", err)
			}
		})
	}
}
