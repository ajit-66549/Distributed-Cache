package cache

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/metrics"
)

func TestGetRecordsHitsAndMisses(t *testing.T) {
	measurements := metrics.New()
	store := NewStore(WithMetrics(measurements))
	if err := store.Set("name", "Ajit"); err != nil {
		t.Fatalf("store name: %v", err)
	}

	// Two successful reads should record two hits.
	for i := 0; i < 2; i++ {
		if value, found := store.Get("name"); !found || value != "Ajit" {
			t.Fatalf("read %d: got (%q, %v), want (Ajit, true)", i+1, value, found)
		}
	}
	// One absent-key read should record one miss.
	if value, found := store.Get("missing"); found || value != "" {
		t.Fatalf("missing read: got (%q, %v), want (empty, false)", value, found)
	}

	// Inspect the exported counters instead of calling recording methods directly.
	families, err := measurements.Gatherer().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	want := map[string]float64{
		"cache_hits_total":   2,
		"cache_misses_total": 1,
	}
	for _, family := range families {
		expected, ok := want[family.GetName()]
		if !ok {
			continue
		}
		samples := family.GetMetric()
		if len(samples) != 1 || samples[0].Counter == nil {
			t.Fatalf("%s: expected one counter sample", family.GetName())
		}
		got := samples[0].GetCounter().GetValue()
		if got != expected {
			t.Errorf("%s = %g, want %g", family.GetName(), got, expected)
		}
		t.Logf("%s = %g", family.GetName(), got)
		delete(want, family.GetName())
	}
	for name := range want {
		t.Errorf("%s was not collected", name)
	}
}

type cacheMetricValues struct {
	Hits, Misses, Evictions             float64
	LazyExpirations, CleanupExpirations float64
	UsedBytes, MaxBytes, Items          float64
}

// Collect exported measurements, including both expiration sources. Heap usage
// is deliberately excluded because allocations and GC make it nondeterministic.
func collectCacheMetricValues(t *testing.T, measurements *metrics.Metrics) cacheMetricValues {
	t.Helper()
	var values cacheMetricValues
	remaining := map[string]*float64{
		"cache_hits_total":                &values.Hits,
		"cache_misses_total":              &values.Misses,
		"cache_evictions_total":           &values.Evictions,
		"cache_expirations_total/lazy":    &values.LazyExpirations,
		"cache_expirations_total/cleanup": &values.CleanupExpirations,
		"cache_used_bytes":                &values.UsedBytes,
		"cache_max_bytes":                 &values.MaxBytes,
		"cache_items":                     &values.Items,
	}
	families, err := measurements.Gatherer().Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		for _, sample := range family.GetMetric() {
			name := family.GetName()
			for _, label := range sample.GetLabel() {
				if label.GetName() == "source" {
					name += "/" + label.GetValue()
				}
			}
			destination, ok := remaining[name]
			if !ok {
				continue
			}
			switch {
			case sample.Counter != nil:
				*destination = sample.GetCounter().GetValue()
			case sample.Gauge != nil:
				*destination = sample.GetGauge().GetValue()
			default:
				t.Fatalf("%s is neither a counter nor a gauge", name)
			}
			delete(remaining, name)
		}
	}
	for name := range remaining {
		t.Errorf("%s was not collected", name)
	}
	return values
}

func TestCacheMetricScenarios(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		budget int64
		run    func(*testing.T, *Store, *snapshotTestLog)
		want   cacheMetricValues
	}{
		{
			name: "store and read", budget: 8,
			run: func(t *testing.T, store *Store, _ *snapshotTestLog) {
				mustSet(t, store, "name", "Ajit")
				for range 2 {
					if value, found := store.Get("name"); !found || value != "Ajit" {
						t.Fatalf("GET name = (%q, %v)", value, found)
					}
				}
			},
			want: cacheMetricValues{Hits: 2, UsedBytes: 8, Items: 1},
		},
		{
			name: "read missing", budget: 8,
			run: func(t *testing.T, store *Store, _ *snapshotTestLog) {
				if value, found := store.Get("missing"); found || value != "" {
					t.Fatalf("missing GET = (%q, %v)", value, found)
				}
			},
			want: cacheMetricValues{Misses: 1},
		},
		{
			name: "empty value is a hit", budget: 8,
			run: func(t *testing.T, store *Store, _ *snapshotTestLog) {
				mustSet(t, store, "name", "")
				if value, found := store.Get("name"); !found || value != "" {
					t.Fatalf("empty-value GET = (%q, %v)", value, found)
				}
			},
			want: cacheMetricValues{Hits: 1, UsedBytes: 4, Items: 1},
		},
		{
			name: "expired entry still occupies space until removal", budget: 8,
			run: func(t *testing.T, store *Store, _ *snapshotTestLog) {
				if err := store.SetWithTTL("name", "Ajit", -time.Second); err != nil {
					t.Fatal(err)
				}
			},
			want: cacheMetricValues{UsedBytes: 8, Items: 1},
		},
		{
			name: "expire and read twice", budget: 8,
			run: func(t *testing.T, store *Store, _ *snapshotTestLog) {
				// An already-passed deadline avoids sleeps and scheduler-dependent TTLs.
				if err := store.SetWithTTL("name", "Ajit", -time.Second); err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if value, found := store.Get("name"); found || value != "" {
						t.Fatalf("expired GET = (%q, %v)", value, found)
					}
				}
				if removed := store.DeleteExpired(); removed != 0 {
					t.Fatalf("cleanup removed an entry already removed by GET: %d", removed)
				}
			},
			want: cacheMetricValues{Misses: 2, LazyExpirations: 1},
		},
		{
			name: "force three evictions", budget: 6,
			run: func(t *testing.T, store *Store, _ *snapshotTestLog) {
				for _, key := range []string{"a", "b", "c"} {
					mustSet(t, store, key, "1")
				}
				mustSet(t, store, "d", "12345") // Six bytes require all three victims.
				assertStoreState(t, store, []string{"d"}, 6)
			},
			want: cacheMetricValues{Evictions: 3, UsedBytes: 6, Items: 1},
		},
		{
			name: "expired LRU victim counts as eviction", budget: 6,
			run: func(t *testing.T, store *Store, _ *snapshotTestLog) {
				if err := store.SetWithTTL("a", "1", -time.Second); err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{"b", "c", "d"} {
					mustSet(t, store, key, "1")
				}
				assertStoreState(t, store, []string{"d", "c", "b"}, 6)
			},
			want: cacheMetricValues{Evictions: 1, UsedBytes: 6, Items: 3},
		},
		{
			name: "replace with a smaller value", budget: 8,
			run: func(t *testing.T, store *Store, _ *snapshotTestLog) {
				mustSet(t, store, "name", "Ajit")
				mustSet(t, store, "name", "Ram")
			},
			want: cacheMetricValues{UsedBytes: 7, Items: 1},
		},
		{
			name: "explicit delete of expired entry", budget: 8,
			run: func(t *testing.T, store *Store, _ *snapshotTestLog) {
				if err := store.SetWithTTL("name", "Ajit", -time.Second); err != nil {
					t.Fatal(err)
				}
				if removed, err := store.Delete("name"); err != nil || !removed {
					t.Fatalf("delete = %v, %v", removed, err)
				}
			},
			want: cacheMetricValues{},
		},
		{
			name: "cleanup counts removals once", budget: 16,
			run: func(t *testing.T, store *Store, _ *snapshotTestLog) {
				for _, key := range []string{"a", "b"} {
					if err := store.SetWithTTL(key, "1", -time.Second); err != nil {
						t.Fatal(err)
					}
				}
				mustSet(t, store, "live", "ok")
				if got := store.DeleteExpired(); got != 2 {
					t.Fatalf("first cleanup removed %d entries, want 2", got)
				}
				if got := store.DeleteExpired(); got != 0 {
					t.Fatalf("second cleanup removed %d entries, want 0", got)
				}
			},
			want: cacheMetricValues{CleanupExpirations: 2, UsedBytes: 6, Items: 1},
		},
		{
			name: "reject oversized replacement", budget: 8,
			run: func(t *testing.T, store *Store, mutationLog *snapshotTestLog) {
				mustSet(t, store, "name", "Ajit")
				if err := store.Set("name", "too large"); !errors.Is(err, ErrEntryTooLarge) {
					t.Fatalf("oversized SET = %v, want ErrEntryTooLarge", err)
				}
				if mutationLog.LastSequence() != 1 {
					t.Fatal("oversized write appended a mutation")
				}
				assertStoreState(t, store, []string{"name"}, 8)
			},
			want: cacheMetricValues{UsedBytes: 8, Items: 1},
		},
		{
			name: "failed durable writes preserve completed eviction count", budget: 6,
			run: func(t *testing.T, store *Store, mutationLog *snapshotTestLog) {
				for _, key := range []string{"a", "b", "c", "d"} {
					mustSet(t, store, key, "1") // The fourth successful write evicts a.
				}
				failure := errors.New("injected persistence failure")
				mutationLog.appendErr = failure
				if err := store.Set("e", "12345"); !errors.Is(err, failure) {
					t.Fatalf("failed insert = %v, want injected failure", err)
				}
				if err := store.Set("b", "12345"); !errors.Is(err, failure) {
					t.Fatalf("failed replacement = %v, want injected failure", err)
				}
				if removed, err := store.Delete("c"); removed || !errors.Is(err, failure) {
					t.Fatalf("failed delete = %v, %v", removed, err)
				}
				if mutationLog.LastSequence() != 4 || len(mutationLog.records) != 4 {
					t.Fatal("failed mutations changed the completed log history")
				}
				assertStoreState(t, store, []string{"d", "c", "b"}, 6)
			},
			want: cacheMetricValues{Evictions: 1, UsedBytes: 6, Items: 3},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			measurements := metrics.New()
			mutationLog := &snapshotTestLog{}
			store, err := NewStoreWithLog(scenario.budget, mutationLog, WithMetrics(measurements))
			if err != nil {
				t.Fatal(err)
			}
			initial := cacheMetricValues{MaxBytes: float64(scenario.budget)}
			if got := collectCacheMetricValues(t, measurements); got != initial {
				t.Fatalf("initial metrics = %+v, want %+v", got, initial)
			}
			scenario.run(t, store, mutationLog)
			if err := storeInvariantError(store); err != nil {
				t.Fatal(err)
			}
			want := scenario.want
			want.MaxBytes = float64(scenario.budget)
			if got := collectCacheMetricValues(t, measurements); got != want {
				t.Errorf("collected metrics = %+v, want %+v", got, want)
			}
		})
	}
}

func TestCacheMetricsConcurrentOperationsAndCollection(t *testing.T) {
	const workers, iterations = 8, 64
	const entryBytes = 7 // key-00 plus a one-byte value.
	const budget = workers * entryBytes
	measurements := metrics.New()
	store, err := NewStoreWithLimit(budget, WithMetrics(measurements))
	if err != nil {
		t.Fatal(err)
	}
	start, done, collected := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var writers sync.WaitGroup
	for worker := range workers {
		writers.Go(func() {
			<-start
			key := fmt.Sprintf("key-%02d", worker)
			for range iterations {
				if err := store.Set(key, "a"); err != nil {
					t.Error(err)
					return
				}
				if value, found := store.Get(key); !found || value != "a" {
					t.Error("concurrent live GET did not return its value")
					return
				}
				if err := store.Set(key, "b"); err != nil {
					t.Error(err)
					return
				}
				if removed, err := store.Delete(key); err != nil || !removed {
					t.Errorf("concurrent DELETE = %v, %v", removed, err)
					return
				}
				if err := store.SetWithTTL(key, "c", -time.Second); err != nil {
					t.Error(err)
					return
				}
				if _, found := store.Get(key); found {
					t.Error("concurrent expired GET returned a value")
					return
				}
			}
		})
	}
	go func() {
		defer close(collected)
		<-start
		for {
			store.DeleteExpired()
			values := collectCacheMetricValues(t, measurements)
			if values.MaxBytes != budget || values.Items < 0 || values.Items > workers || values.UsedBytes != entryBytes*values.Items {
				t.Errorf("inconsistent size snapshot during concurrent updates: %+v", values)
				return
			}
			select {
			case <-done:
				return
			default:
			}
		}
	}()
	close(start)
	writers.Wait()
	close(done)
	<-collected
	values := collectCacheMetricValues(t, measurements)
	const total = workers * iterations
	if values.Hits != total || values.Misses != total || values.Evictions != 0 || values.UsedBytes != 0 || values.Items != 0 || values.MaxBytes != budget {
		t.Errorf("unexpected final concurrent metrics: %+v", values)
	}
	// GET and cleanup race to remove each expired entry; exactly one must count it.
	if values.LazyExpirations+values.CleanupExpirations != total {
		t.Errorf("expiration removals = %g, want %d", values.LazyExpirations+values.CleanupExpirations, total)
	}
}
