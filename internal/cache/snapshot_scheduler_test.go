package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/persistence"
)

func schedulerResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("snapshot worker did not finish")
		return nil
	}
}

func TestSnapshotSchedulerRunsPeriodicallyAndCancels(t *testing.T) {
	saves := make(chan time.Time, 8)
	log := &snapshotTestLog{onSave: func(persistence.Snapshot) error {
		select {
		case saves <- time.Now():
		default:
		}
		return nil
	}}
	store, err := NewStoreWithLog(128, log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interval := 20 * time.Millisecond
	started := time.Now()
	done, err := StartSnapshotScheduler(ctx, store, interval)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		select {
		case savedAt := <-saves:
			if index == 0 && savedAt.Sub(started) < interval {
				t.Fatal("first snapshot ran before its interval")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("periodic snapshot did not run")
		}
	}
	cancel()
	if err := schedulerResult(t, done); err != nil {
		t.Fatal(err)
	}
	select {
	case _, open := <-done:
		if open {
			t.Fatal("completion channel was not closed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("completion channel did not close")
	}
}

func TestSnapshotSchedulerWaitsForActiveSaveOnCancellation(t *testing.T) {
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	var releaseOnce sync.Once
	var calls atomic.Int32
	log := &snapshotTestLog{onSave: func(persistence.Snapshot) error {
		calls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return nil
	}}
	store, err := NewStoreWithLog(128, log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
	})
	done, err := StartSnapshotScheduler(ctx, store, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("snapshot save did not begin")
	}
	// Leave several tick intervals inside one blocked save.
	select {
	case <-entered:
		t.Fatal("snapshot saves overlapped")
	case <-time.After(10 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
		t.Fatal("worker reported completion before its active save finished")
	case <-time.After(10 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	if err := schedulerResult(t, done); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("save calls = %d, want exactly one without queued saves", got)
	}
}

func TestSnapshotSchedulerStopsOnSaveFailure(t *testing.T) {
	failure := errors.New("injected snapshot failure")
	var calls atomic.Int32
	log := &snapshotTestLog{onSave: func(persistence.Snapshot) error {
		calls.Add(1)
		return failure
	}}
	store, err := NewStoreWithLog(128, log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done, err := StartSnapshotScheduler(ctx, store, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := schedulerResult(t, done); !errors.Is(err, failure) {
		t.Fatalf("worker result = %v, want original save failure", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("failed save was retried %d times", got)
	}
}

func TestSnapshotSchedulerRejectsInvalidArguments(t *testing.T) {
	for _, test := range []struct {
		name     string
		ctx      context.Context
		store    *Store
		interval time.Duration
	}{
		{name: "nil context", store: NewStore(), interval: time.Second},
		{name: "nil store", ctx: context.Background(), interval: time.Second},
		{name: "zero interval", ctx: context.Background(), store: NewStore()},
		{name: "negative interval", ctx: context.Background(), store: NewStore(), interval: -time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			if done, err := StartSnapshotScheduler(test.ctx, test.store, test.interval); err == nil || done != nil {
				t.Fatalf("StartSnapshotScheduler() = %v, %v; want nil channel and an error", done, err)
			}
		})
	}
}
