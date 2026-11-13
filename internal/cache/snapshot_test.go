package cache

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/persistence"
)

type snapshotTestLog struct {
	mu        sync.Mutex
	sequence  uint64
	records   []persistence.Record
	appendErr error
	onSave    func(persistence.Snapshot) error
}

func (log *snapshotTestLog) LastSequence() uint64 {
	log.mu.Lock()
	defer log.mu.Unlock()
	return log.sequence
}

func (log *snapshotTestLog) Append(record persistence.Record) error {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.appendErr != nil {
		return log.appendErr
	}
	log.sequence = record.Sequence
	log.records = append(log.records, record)
	return nil
}

func (log *snapshotTestLog) SaveSnapshot(snapshot persistence.Snapshot) error {
	if log.onSave != nil {
		return log.onSave(snapshot)
	}
	return nil
}

func TestSnapshotCapturePreservesLiveStateAndLRU(t *testing.T) {
	var captured persistence.Snapshot
	log := &snapshotTestLog{onSave: func(snapshot persistence.Snapshot) error {
		captured = snapshot
		return nil
	}}
	store, err := NewStoreWithLog(512, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("a", "one"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWithTTL("b", "two", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWithTTL("expired", "old", -time.Hour); err != nil {
		t.Fatal(err)
	}
	store.Get("a") // Reading changes recency without changing the log sequence.
	expectedDeadline := store.entries["b"].Value.(*lruItem).Entry.ExpiresAt.UnixNano()
	if err := store.SaveSnapshot(); err != nil {
		t.Fatal(err)
	}
	if captured.Header.Checkpoint != 3 || captured.Header.EntryCount != 2 || captured.Header.MaxBytes != 512 {
		t.Fatalf("unexpected snapshot metadata: %+v", captured.Header)
	}
	var keys []string
	for _, entry := range captured.Entries {
		keys = append(keys, entry.Key)
	}
	if !reflect.DeepEqual(keys, []string{"b", "a"}) {
		t.Fatalf("saved LRU order = %v, want [b a]", keys)
	}
	if captured.Entries[0].ExpiresAtUnixNano == nil || *captured.Entries[0].ExpiresAtUnixNano != expectedDeadline {
		t.Fatal("capture changed the original expiration deadline")
	}
	if captured.Entries[1].ExpiresAtUnixNano != nil {
		t.Fatal("permanent entry unexpectedly acquired an expiration")
	}
	if store.Len() != 3 || len(log.records) != 3 {
		t.Fatal("capture changed cache membership or appended mutation records")
	}
}

func TestSnapshotRestorePreservesDeadlineAndEvictionOrder(t *testing.T) {
	past := time.Now().Add(-time.Hour).UnixNano()
	future := time.Now().Add(time.Hour).UnixNano()
	log := &snapshotTestLog{sequence: 7}
	store, err := NewStoreWithLog(10, log)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := persistence.Snapshot{
		Header: persistence.SnapshotHeader{Version: persistence.SnapshotFormatVersion, Checkpoint: 7, MaxBytes: 10, EntryCount: 3},
		Entries: []persistence.SnapshotEntry{
			{Key: "a", Value: "1", ExpiresAtUnixNano: &past},
			{Key: "b", Value: "1", ExpiresAtUnixNano: &future},
			{Key: "c", Value: "1"},
		},
	}
	if err := store.RestoreSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if store.Len() != 2 || store.usedBytes != 4 || len(log.records) != 0 {
		t.Fatal("restore resurrected an expired entry, miscounted bytes, or logged mutations")
	}
	if got := store.entries["b"].Value.(*lruItem).Entry.ExpiresAt.UnixNano(); got != future {
		t.Fatalf("restored deadline = %d, want %d", got, future)
	}
	if err := store.Set("d", "1234567"); err != nil {
		t.Fatal(err)
	}
	if _, found := store.Get("b"); found {
		t.Fatal("the least recently used live snapshot entry was not evicted")
	}
	if _, found := store.Get("c"); !found {
		t.Fatal("a more recently used entry was evicted instead")
	}
	if store.usedBytes != 10 {
		t.Fatalf("byte count after eviction = %d, want 10", store.usedBytes)
	}
}

func TestFailedMutationDoesNotApplyEvictionsOrDeletion(t *testing.T) {
	log := &snapshotTestLog{}
	store, err := NewStoreWithLog(4, log)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a", "b"} {
		if err := store.Set(key, "1"); err != nil {
			t.Fatal(err)
		}
	}
	failure := errors.New("injected append failure")
	log.appendErr = failure
	if err := store.Set("c", "1"); !errors.Is(err, failure) {
		t.Fatalf("Set() = %v, want append failure", err)
	}
	if deleted, err := store.Delete("b"); deleted || !errors.Is(err, failure) {
		t.Fatalf("Delete() = %v, %v; want false, append failure", deleted, err)
	}
	if store.Len() != 2 || store.usedBytes != 4 || log.LastSequence() != 2 {
		t.Fatal("failed mutations changed membership, accounting, or sequence")
	}
	for _, key := range []string{"a", "b"} {
		if value, found := store.Get(key); !found || value != "1" {
			t.Fatalf("original key %q was changed by a failed mutation", key)
		}
	}
	if _, found := store.Get("c"); found {
		t.Fatal("failed SET was applied")
	}
}
