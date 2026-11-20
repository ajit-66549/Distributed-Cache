//go:build darwin || linux

package cache

import (
	"fmt"
	"testing"

	"github.com/ajit-66549/distributed-cache/internal/persistence"
)

func preparePersistenceBenchmark(b *testing.B, mode string) (*Store, []string) {
	b.Helper()
	const entries = 16
	keys := make([]string, entries+1)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%06d", i)
	}
	budget := int64(entries * (len(keys[0]) + len(benchmarkValue)))
	store := newLimitedStore(b, budget)
	if mode == "aof-sync" {
		log, err := persistence.OpenAOF(b.TempDir(), budget)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() {
			if err := log.Close(); err != nil {
				b.Error(err)
			}
		})
		store, err = NewStoreWithLog(budget, log)
		if err != nil {
			b.Fatal(err)
		}
	}
	for _, key := range keys[:entries] {
		if err := store.Set(key, benchmarkValue); err != nil {
			b.Fatal(err)
		}
	}
	return store, keys
}

// Run identical mutations with and without a real AOF. Opening, prepopulation,
// and closing are excluded; each timed durable write includes os.File.Sync.
func BenchmarkPersistence(b *testing.B) {
	for _, mode := range []string{"memory", "aof-sync"} {
		for _, workload := range []string{"set-update", "set-evict", "set-delete-cycle", "parallel-set-update"} {
			b.Run(fmt.Sprintf("%s/%s/capacity=16", mode, workload), func(b *testing.B) {
				store, keys := preparePersistenceBenchmark(b, mode)
				if workload == "set-delete-cycle" {
					// Reserve a slot so every cycle inserts without eviction.
					deleted, err := store.Delete(keys[0])
					if err != nil || !deleted {
						b.Fatalf("reserve slot: removed=%t, error=%v", deleted, err)
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				if workload == "parallel-set-update" {
					b.RunParallel(func(pb *testing.PB) {
						i := 0
						for pb.Next() {
							if err := store.Set(keys[i%16], benchmarkValue); err != nil {
								b.Error(err)
								return
							}
							i++
						}
					})
				} else {
					for i := 0; i < b.N; i++ {
						key := keys[i%16]
						switch workload {
						case "set-evict":
							// The seventeenth key rotates through a full cache.
							key = keys[(i+16)%len(keys)]
						case "set-delete-cycle":
							// Reinsert the extra key, then delete it each cycle.
							key = keys[16]
						}
						if err := store.Set(key, benchmarkValue); err != nil {
							b.Fatal(err)
						}
						if workload == "set-delete-cycle" {
							deleted, err := store.Delete(key)
							if err != nil || !deleted {
								b.Fatalf("delete: removed=%t, error=%v", deleted, err)
							}
						}
					}
				}
				b.StopTimer()
				writes := 1.0
				if workload == "set-delete-cycle" {
					writes = 2
				}
				b.ReportMetric(writes, "writes/op")
				syncs := 0.0
				if mode == "aof-sync" {
					syncs = writes
				}
				b.ReportMetric(syncs, "syncs/op")
			})
		}
	}
}
