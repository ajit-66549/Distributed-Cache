//go:build darwin || linux

package persistence

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAOFWritableLifecycleAndValidation(t *testing.T) {
	if (&AOF{}).Writable() {
		t.Fatal("unopened writer is writable")
	}
	writer := openRecoveryAOF(t, t.TempDir())
	if !writer.Writable() {
		t.Fatal("healthy opened writer is not writable")
	}
	if err := writer.Append(recoverySet(2, "name", "Ajit")); !errors.Is(err, ErrInvalidSequence) {
		t.Fatalf("invalid sequence: %v", err)
	}
	if !writer.Writable() {
		t.Fatal("validation rejection disabled the writer")
	}
	if err := writer.SaveSnapshot(Snapshot{Header: SnapshotHeader{Version: SnapshotFormatVersion, MaxBytes: recoveryTestBudget}}); err != nil {
		t.Fatal(err)
	}
	if !writer.Writable() {
		t.Fatal("successful snapshot disabled the writer")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if writer.Writable() {
		t.Fatal("closed writer remained writable")
	}
}

func TestAOFWritableReflectsTerminalIOFailures(t *testing.T) {
	for _, operation := range []string{"append", "sync", "snapshot"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			writer := openRecoveryAOF(t, dir)
			var err error
			if operation == "snapshot" {
				if err := os.Mkdir(filepath.Join(dir, snapshotFilename), 0700); err != nil {
					t.Fatal(err)
				}
				err = writer.SaveSnapshot(Snapshot{Header: SnapshotHeader{Version: SnapshotFormatVersion, MaxBytes: recoveryTestBudget}})
			} else {
				if err := writer.file.Close(); err != nil {
					t.Fatal(err)
				}
				if operation == "append" {
					err = writer.Append(recoverySet(1, "name", "Ajit"))
				} else {
					err = writer.Sync()
				}
			}
			if !errors.Is(err, ErrAOFFailed) || writer.Writable() {
				t.Fatalf("terminal %s failure: err=%v writable=%v", operation, err, writer.Writable())
			}
			if err := writer.Sync(); !errors.Is(err, ErrAOFFailed) || writer.Writable() {
				t.Fatalf("failed writer became healthy again: %v", err)
			}
		})
	}
}

func TestAOFWritableDoesNotWaitForPersistenceLock(t *testing.T) {
	writer := openRecoveryAOF(t, t.TempDir())
	writer.mu.Lock()
	defer writer.mu.Unlock()
	result := make(chan bool, 1)
	go func() { result <- writer.Writable() }()
	select {
	case ready := <-result:
		if !ready {
			t.Fatal("healthy writer reported unavailable while locked")
		}
	case <-time.After(time.Second):
		t.Fatal("writability check waited for the persistence mutex")
	}
}

func TestAOFWritableReflectsSequenceExhaustion(t *testing.T) {
	for _, base := range []uint64{math.MaxUint64 - 1, math.MaxUint64} {
		dir := t.TempDir()
		writeRecoveryFixture(t, filepath.Join(dir, aofFilename), aofFixture(t, base))
		snapshot := Snapshot{Header: SnapshotHeader{Version: SnapshotFormatVersion, Checkpoint: base, MaxBytes: recoveryTestBudget}}
		writeRecoveryFixture(t, filepath.Join(dir, snapshotFilename), snapshotFixture(t, snapshot))
		writer, _, err := OpenAOFWithSnapshot(dir, recoveryTestBudget)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = writer.Close() })
		if base < math.MaxUint64 {
			if !writer.Writable() {
				t.Fatal("last available sequence was rejected")
			}
			if err := writer.Append(recoverySet(math.MaxUint64, "last", "value")); err != nil {
				t.Fatal(err)
			}
		}
		if writer.Writable() {
			t.Fatal("exhausted sequence remained writable")
		}
	}
}
