//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/cache"
	"github.com/ajit-66549/distributed-cache/internal/config"
)

func startupTestConfig(dir string, maxBytes int64) config.Config {
	return config.Config{Port: "8080", DataDir: dir, MaxBytes: maxBytes, SnapshotInterval: time.Minute}
}

func assertRecoveredStartupState(t *testing.T, store *cache.Store) {
	t.Helper()
	for key, want := range map[string]string{"name": "Amit", "later": "saved"} {
		if got, found := store.Get(key); !found || got != want {
			t.Fatalf("recovered %q = %q, %v; want %q", key, got, found, want)
		}
	}
	for _, key := range []string{"city", "session"} {
		if _, found := store.Get(key); found {
			t.Fatalf("deleted or expired key %q was resurrected", key)
		}
	}
	if store.Len() != 2 {
		t.Fatalf("recovered entry count = %d, want 2", store.Len())
	}
}

func TestOpenStoreRecoversAfterAbruptExit(t *testing.T) {
	const childExitCode = 23
	if os.Getenv("CACHE_RECOVERY_TEST_CHILD") == "1" {
		store, _, err := openStore(startupTestConfig(os.Getenv("CACHE_RECOVERY_TEST_DIR"), 512))
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range map[string]string{"name": "Ajit", "session": "old", "city": "Pune"} {
			if err := store.Set(key, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.SaveSnapshot(); err != nil {
			t.Fatal(err)
		}
		if err := store.Set("name", "Amit"); err != nil {
			t.Fatal(err)
		}
		if deleted, err := store.Delete("city"); err != nil || !deleted {
			t.Fatalf("delete city = %v, %v", deleted, err)
		}
		if err := store.SetWithTTL("session", "expired", -time.Hour); err != nil {
			t.Fatal(err)
		}
		if err := store.Set("later", "saved"); err != nil {
			t.Fatal(err)
		}
		// Deliberately bypass writer.Close and all deferred shutdown cleanup.
		os.Exit(childExitCode)
	}

	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOpenStoreRecoversAfterAbruptExit$")
	child.Env = append(os.Environ(), "CACHE_RECOVERY_TEST_CHILD=1", "CACHE_RECOVERY_TEST_DIR="+dir)
	output, err := child.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != childExitCode {
		t.Fatalf("writer subprocess did not reach its abrupt exit: %v\n%s", err, output)
	}
	path := filepath.Join(dir, "append.aof")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store, writer, err := openStore(startupTestConfig(dir, 512))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	assertRecoveredStartupState(t, store)
	if writer.Header().BaseSequence != 3 || writer.LastSequence() != 7 {
		t.Fatal("restart lost the snapshot checkpoint or later mutation sequence")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("recovery rewrote or re-logged AOF records: %v", err)
	}
	if err := store.SaveSnapshot(); err != nil {
		t.Fatal(err)
	}
	if writer.Header().BaseSequence != 7 {
		t.Fatal("second snapshot did not advance the rotation checkpoint")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	restored, reopened, err := openStore(startupTestConfig(dir, 512))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	assertRecoveredStartupState(t, restored)
}

func TestOpenStorePreservesSnapshotLRUWithOldAOF(t *testing.T) {
	dir := t.TempDir()
	cfg := startupTestConfig(dir, 10)
	store, writer, err := openStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	for _, key := range []string{"a", "b", "c"} {
		if err := store.Set(key, "1"); err != nil {
			t.Fatal(err)
		}
	}
	store.Get("a") // Snapshot order is b,c,a; the AOF's SET order is a,b,c.
	path := filepath.Join(dir, "append.aof")
	oldLog, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	// Model a stop after publishing the snapshot but before replacing the AOF.
	if err := os.WriteFile(path, oldLog, 0600); err != nil {
		t.Fatal(err)
	}
	restored, reopened, err := openStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := restored.Set("d", "1234567"); err != nil {
		t.Fatal(err)
	}
	for key, wantFound := range map[string]bool{"a": true, "b": false, "c": false, "d": true} {
		if _, found := restored.Get(key); found != wantFound {
			t.Fatalf("key %q found = %v, want %v; covered AOF records may have reset LRU order", key, found, wantFound)
		}
	}
}

func TestConcurrentSnapshotsAndMutationsRecoverFinalState(t *testing.T) {
	cfg := startupTestConfig(t.TempDir(), 4096)
	store, writer, err := openStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	start := make(chan struct{})
	failures := make(chan error, 4)
	var workers sync.WaitGroup
	for worker := 0; worker < 3; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			<-start
			for index := 0; index < 12; index++ {
				key := fmt.Sprintf("worker-%d-key-%d", worker, index)
				if err := store.Set(key, "saved"); err != nil {
					failures <- err
					return
				}
				if writer.Header().MaxBytes != cfg.MaxBytes {
					failures <- errors.New("rotation changed the memory budget")
					return
				}
				if index%2 == 0 {
					if deleted, err := store.Delete(key); err != nil || !deleted {
						failures <- fmt.Errorf("delete %q: deleted=%v, err=%v", key, deleted, err)
						return
					}
				}
			}
		}(worker)
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for index := 0; index < 3; index++ {
			if err := store.SaveSnapshot(); err != nil {
				failures <- err
				return
			}
		}
	}()
	close(start)
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	if writer.LastSequence() != 54 {
		t.Fatalf("sequence after concurrent work = %d, want 54", writer.LastSequence())
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	restored, reopened, err := openStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if restored.Len() != 18 {
		t.Fatalf("restored %d entries, want 18", restored.Len())
	}
	for worker := 0; worker < 3; worker++ {
		for index := 0; index < 12; index++ {
			key := fmt.Sprintf("worker-%d-key-%d", worker, index)
			value, found := restored.Get(key)
			if found != (index%2 != 0) || found && value != "saved" {
				t.Fatalf("wrong recovered state for %q: %q, %v", key, value, found)
			}
		}
	}
}
