//go:build darwin || linux

package persistence

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

const recoveryTestBudget int64 = 128

func recoverySet(sequence uint64, key, value string) Record {
	return Record{Sequence: sequence, Mutations: []Mutation{{Op: OpSet, Key: key, Value: value}}}
}

func recordFixture(t *testing.T, record Record) []byte {
	t.Helper()
	data, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func aofFixture(t *testing.T, base uint64, records ...Record) []byte {
	t.Helper()
	data, err := EncodeHeader(Header{Version: FormatVersion, BaseSequence: base, MaxBytes: recoveryTestBudget})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		data = append(data, recordFixture(t, record)...)
	}
	return data
}

func writeRecoveryFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func readRecoveryFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func openRecoveryAOF(t *testing.T, dir string) *AOF {
	t.Helper()
	writer, err := OpenAOF(dir, recoveryTestBudget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	return writer
}

func TestAOFRecoveryKeepsCompletePrefix(t *testing.T) {
	first := recoverySet(1, "first", "saved")
	second := recoverySet(2, "second", "saved")
	prefix := aofFixture(t, 0, first)
	frame := recordFixture(t, second)
	for _, cut := range []int{0, 1, 7, 8, 10, len(frame) - 2} {
		t.Run(strconv.Itoa(cut), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, aofFilename)
			data := append(bytes.Clone(prefix), frame[:cut]...)
			writeRecoveryFixture(t, path, data)
			writer := openRecoveryAOF(t, dir)
			if writer.LastSequence() != 1 || !bytes.Equal(readRecoveryFixture(t, path), prefix) {
				t.Fatal("recovery did not retain exactly the complete prefix")
			}
			var recovered []Record
			if err := writer.Replay(func(record Record) error {
				recovered = append(recovered, record)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(recovered, []Record{first}) {
				t.Fatalf("recovered records = %+v, want only the complete first record", recovered)
			}
			if err := writer.Append(second); err != nil {
				t.Fatalf("append after repair: %v", err)
			}
			if !bytes.Equal(readRecoveryFixture(t, path), append(bytes.Clone(prefix), frame...)) {
				t.Fatal("append after repair left incomplete bytes or damaged existing records")
			}
		})
	}
}

func TestAOFCorruptionDoesNotModifyTheFile(t *testing.T) {
	valid := aofFixture(t, 0, recoverySet(1, "name", "Amit"))
	for _, test := range []struct {
		name   string
		damage func([]byte) []byte
		want   error
	}{
		{"empty header", func([]byte) []byte { return nil }, ErrInvalidHeader},
		{"partial header", func(data []byte) []byte { return data[:7] }, ErrInvalidHeader},
		{"header checksum", func(data []byte) []byte { data[10] ^= 1; return data }, ErrChecksumMismatch},
		{"length checksum", func(data []byte) []byte { data[HeaderSize+4] ^= 1; return data }, ErrChecksumMismatch},
		{"complete corrupt tail", func(data []byte) []byte { data[len(data)-1] ^= 1; return data }, ErrChecksumMismatch},
		{"sequence gap", func(data []byte) []byte { return append(data, recordFixture(t, recoverySet(3, "city", "Pune"))...) }, ErrInvalidSequence},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, aofFilename)
			data := test.damage(bytes.Clone(valid))
			writeRecoveryFixture(t, path, data)
			writer, err := OpenAOF(dir, recoveryTestBudget)
			if writer != nil {
				_ = writer.Close()
				t.Fatal("corrupt AOF was opened")
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("OpenAOF() = %v, want %v", err, test.want)
			}
			if !bytes.Equal(readRecoveryFixture(t, path), data) {
				t.Fatal("rejected corruption was silently repaired or removed")
			}
		})
	}
}

func TestSnapshotRotationPreservesSequenceAndAppendHandle(t *testing.T) {
	for _, checkpoint := range []uint64{0, 2} {
		t.Run(strconv.FormatUint(checkpoint, 10), func(t *testing.T) {
			dir := t.TempDir()
			writer := openRecoveryAOF(t, dir)
			for sequence := uint64(1); sequence <= checkpoint; sequence++ {
				if err := writer.Append(recoverySet(sequence, "name", "Amit")); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := Snapshot{Header: SnapshotHeader{Version: SnapshotFormatVersion, Checkpoint: checkpoint, MaxBytes: recoveryTestBudget}}
			if checkpoint != 0 {
				snapshot.Header.EntryCount = 1
				snapshot.Entries = []SnapshotEntry{{Key: "name", Value: "Amit"}}
			}
			if err := writer.SaveSnapshot(snapshot); err != nil {
				t.Fatal(err)
			}
			if writer.Header().BaseSequence != checkpoint || writer.LastSequence() != checkpoint {
				t.Fatal("rotation changed the sequence or published the wrong base")
			}
			if got := len(readRecoveryFixture(t, filepath.Join(dir, aofFilename))); got != HeaderSize {
				t.Fatalf("rotated AOF size = %d, want header-only size %d", got, HeaderSize)
			}
			next := recoverySet(checkpoint+1, "later", "saved")
			if err := writer.Append(next); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, saved, err := OpenAOFWithSnapshot(dir, recoveryTestBudget)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			if saved == nil || !reflect.DeepEqual(*saved, snapshot) || reopened.LastSequence() != checkpoint+1 {
				t.Fatal("restart lost the snapshot, checkpoint, or post-rotation append")
			}
			var replayed []Record
			if err := reopened.ReplayAfter(checkpoint, func(record Record) error {
				replayed = append(replayed, record)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(replayed, []Record{next}) {
				t.Fatalf("post-checkpoint replay = %+v, want %+v", replayed, next)
			}
		})
	}
}

func TestRotationFailureStopsFurtherAppends(t *testing.T) {
	dir := t.TempDir()
	writer := openRecoveryAOF(t, dir)
	if err := writer.Append(recoverySet(1, "name", "Amit")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, aofFilename)
	oldPath := filepath.Join(dir, "retained.aof")
	before := readRecoveryFixture(t, path)
	if err := os.Rename(path, oldPath); err != nil {
		t.Fatal(err)
	}
	// A directory at the destination reliably rejects replacement on both OSes.
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{
		Header:  SnapshotHeader{Version: SnapshotFormatVersion, Checkpoint: 1, MaxBytes: recoveryTestBudget, EntryCount: 1},
		Entries: []SnapshotEntry{{Key: "name", Value: "Amit"}},
	}
	if err := writer.SaveSnapshot(snapshot); !errors.Is(err, ErrAOFFailed) {
		t.Fatalf("rotation failure = %v, want ErrAOFFailed", err)
	}
	if err := writer.Append(recoverySet(2, "later", "value")); !errors.Is(err, ErrAOFFailed) {
		t.Fatalf("append after failed rotation = %v", err)
	}
	if !bytes.Equal(readRecoveryFixture(t, oldPath), before) {
		t.Fatal("failed rotation or later append changed the retired handle's file")
	}
	saved, err := DecodeSnapshot(bytes.NewReader(readRecoveryFixture(t, filepath.Join(dir, snapshotFilename))), recoveryTestBudget)
	if err != nil || saved.Header.Checkpoint != 1 {
		t.Fatalf("snapshot was not durable before rotation: %+v, %v", saved.Header, err)
	}
	for _, pattern := range []string{".aof-*.tmp", ".snapshot-*.tmp"} {
		leftovers, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil || len(leftovers) != 0 {
			t.Fatalf("temporary file cleanup failed: %v, %v", leftovers, err)
		}
	}
}

func TestInconsistentSnapshotHistoryIsRejectedBeforeTailRepair(t *testing.T) {
	for _, test := range []struct {
		name       string
		base       uint64
		checkpoint uint64
		missingAOF bool
		corrupt    bool
		want       error
	}{
		{name: "snapshot without log", checkpoint: 1, missingAOF: true, want: ErrRecoveryRequired},
		{name: "base ahead of snapshot", base: 2, checkpoint: 1, want: ErrRecoveryRequired},
		{name: "snapshot ahead of log", checkpoint: 2, want: ErrInvalidSequence},
		{name: "corrupt snapshot", checkpoint: 1, corrupt: true, want: ErrChecksumMismatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			snapshot := Snapshot{
				Header:  SnapshotHeader{Version: SnapshotFormatVersion, Checkpoint: test.checkpoint, MaxBytes: recoveryTestBudget, EntryCount: 1},
				Entries: []SnapshotEntry{{Key: "name", Value: "Amit"}},
			}
			snapshotData := snapshotFixture(t, snapshot)
			if test.corrupt {
				snapshotData[len(snapshotData)-1] ^= 1
			}
			snapshotPath := filepath.Join(dir, snapshotFilename)
			writeRecoveryFixture(t, snapshotPath, snapshotData)
			path := filepath.Join(dir, aofFilename)
			data := aofFixture(t, test.base)
			if test.base == 0 {
				data = aofFixture(t, 0, recoverySet(1, "name", "Amit"))
			}
			partial := recordFixture(t, recoverySet(3, "later", "value"))
			data = append(data, partial[:10]...)
			if !test.missingAOF {
				writeRecoveryFixture(t, path, data)
			}
			writer, _, err := OpenAOFWithSnapshot(dir, recoveryTestBudget)
			if writer != nil {
				_ = writer.Close()
				t.Fatal("inconsistent snapshot/log pair was opened")
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("open error = %v, want %v", err, test.want)
			}
			if test.missingAOF {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("startup created an AOF beside a snapshot: %v", err)
				}
			} else if !bytes.Equal(readRecoveryFixture(t, path), data) {
				t.Fatal("startup repaired a tail before rejecting incompatible files")
			}
			if !bytes.Equal(readRecoveryFixture(t, snapshotPath), snapshotData) {
				t.Fatal("startup modified the rejected snapshot")
			}
		})
	}
}

func TestAOFDirectoryLockIsReleasedOnClose(t *testing.T) {
	dir := t.TempDir()
	writer := openRecoveryAOF(t, dir)
	second, err := OpenAOF(dir, recoveryTestBudget)
	if second != nil {
		_ = second.Close()
		t.Fatal("two writers acquired the same data directory")
	}
	if !errors.Is(err, ErrAOFInUse) {
		t.Fatalf("second open = %v, want ErrAOFInUse", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	openRecoveryAOF(t, dir)
	if err := writer.Append(recoverySet(1, "closed", "value")); !errors.Is(err, ErrAOFClosed) {
		t.Fatalf("closed writer accepted append: %v", err)
	}
}
