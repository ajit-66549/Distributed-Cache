package cache

import (
	"fmt"
	"testing"
	"time"
)

// These workloads use memory-only stores, without metrics or background workers.
func BenchmarkGetMissMemory(b *testing.B) {
	for _, entries := range []int{16, 1024, 65536} {
		b.Run(fmt.Sprintf("entries=%d", entries), func(b *testing.B) {
			store, keys := prepareBenchmarkStore(b, entries, entries+1)
			missing := keys[entries]
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, found := store.Get(missing); found {
					b.Fatal("expected a cache miss")
				}
			}
		})
	}
}

func BenchmarkDeleteMemory(b *testing.B) {
	// Successful deletes drain a batch; refilling is excluded from both time
	// and allocation measurements. Occupancy falls from batch size to zero.
	for _, batch := range []int{1024, 65536} {
		b.Run(fmt.Sprintf("hit/batch=%d", batch), func(b *testing.B) {
			store, keys := prepareBenchmarkStore(b, batch, batch)
			b.ReportAllocs()
			b.ResetTimer()
			for completed := 0; completed < b.N; {
				count := min(len(keys), b.N-completed)
				for _, key := range keys[:count] {
					deleted, err := store.Delete(key)
					if err != nil || !deleted {
						b.Fatalf("delete: removed=%t, error=%v", deleted, err)
					}
				}
				completed += count
				if completed < b.N {
					b.StopTimer()
					for _, key := range keys {
						if err := store.Set(key, benchmarkValue); err != nil {
							b.Fatal(err)
						}
					}
					b.StartTimer()
				}
			}
		})
	}
	for _, entries := range []int{16, 1024, 65536} {
		b.Run(fmt.Sprintf("miss/entries=%d", entries), func(b *testing.B) {
			store, keys := prepareBenchmarkStore(b, entries, entries+1)
			missing := keys[entries]
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				deleted, err := store.Delete(missing)
				if err != nil || deleted {
					b.Fatalf("delete missing: removed=%t, error=%v", deleted, err)
				}
			}
		})
	}
}

func BenchmarkTTLCleanupMemory(b *testing.B) {
	for _, entries := range []int{1024, 65536} {
		for _, percent := range []int{0, 50, 100} {
			b.Run(fmt.Sprintf("entries=%d/expired=%dpct", entries, percent), func(b *testing.B) {
				store, keys := prepareBenchmarkStore(b, entries, entries)
				expired := entries * percent / 100
				refillExpired := func() {
					for _, key := range keys[:expired] {
						// Past deadlines avoid sleeps and expiration races.
						if err := store.SetWithTTL(key, benchmarkValue, -time.Second); err != nil {
							b.Fatal(err)
						}
					}
				}
				refillExpired()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if deleted := store.DeleteExpired(); deleted != expired {
						b.Fatalf("cleanup removed %d entries, want %d", deleted, expired)
					}
					if expired > 0 && i+1 < b.N {
						b.StopTimer()
						refillExpired()
						b.StartTimer()
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(entries), "scanned/op")
				b.ReportMetric(float64(expired), "removed/op")
			})
		}
	}
}

func BenchmarkParallelMemory(b *testing.B) {
	for _, workload := range []string{"get-hit", "get-miss", "set-update", "mixed-90get-10set"} {
		for _, entries := range []int{1024, 65536} {
			b.Run(fmt.Sprintf("%s/entries=%d", workload, entries), func(b *testing.B) {
				store, keys := prepareBenchmarkStore(b, entries, entries+1)
				missing := keys[entries]
				keys = keys[:entries]
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					i := 0
					for pb.Next() {
						key := keys[i%len(keys)]
						switch {
						case workload == "get-miss":
							if _, found := store.Get(missing); found {
								b.Error("expected a cache miss")
								return
							}
						case workload == "set-update" || (workload == "mixed-90get-10set" && i%10 == 0):
							if err := store.Set(key, benchmarkValue); err != nil {
								b.Error(err)
								return
							}
						default:
							if value, found := store.Get(key); !found || value != benchmarkValue {
								b.Error("expected a cache hit")
								return
							}
						}
						i++
					}
				})
			})
		}
	}
}
