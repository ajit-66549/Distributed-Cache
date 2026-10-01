package cache

import (
	"fmt"
	"strings"
	"testing"
)

const benchmarkValue = "0123456789abcdef0123456789abcdef"

func prepareBenchmarkStore(b *testing.B, entries, keyCount int) (*Store, []string) {
	b.Helper()
	keys := make([]string, keyCount)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%06d", i)
	}
	entrySize := int64(len(keys[0]) + len(benchmarkValue))
	store := newLimitedStore(b, int64(entries)*entrySize)
	for _, key := range keys[:entries] {
		if err := store.Set(key, benchmarkValue); err != nil {
			b.Fatal(err)
		}
	}
	return store, keys
}

func BenchmarkGet(b *testing.B) {
	for _, entries := range []int{16, 1024, 65536} {
		b.Run(fmt.Sprintf("entries=%d", entries), func(b *testing.B) {
			store, keys := prepareBenchmarkStore(b, entries, entries)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				value, found := store.Get(keys[i%len(keys)])
				if !found || value != benchmarkValue {
					b.Fatal("expected a cache hit")
				}
			}
		})
	}
}

func BenchmarkSet(b *testing.B) {
	for _, operation := range []string{"update", "evict"} {
		for _, entries := range []int{16, 1024, 65536} {
			b.Run(fmt.Sprintf("%s/entries=%d", operation, entries), func(b *testing.B) {
				keyCount := entries
				if operation == "evict" {
					keyCount++
				}
				store, keys := prepareBenchmarkStore(b, entries, keyCount)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					// With one extra key, each write inserts a missing key and
					// evicts the oldest key. Updating uses only existing keys.
					if err := store.Set(keys[(i+entries)%len(keys)], benchmarkValue); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// Each measured cycle fills the cache with small entries, then writes one
// entry that fills the entire budget. Timing includes all writes in the cycle.
func BenchmarkSetEvictionCycle(b *testing.B) {
	for _, entries := range []int{8, 64, 512} {
		b.Run(fmt.Sprintf("victims=%d", entries), func(b *testing.B) {
			store, keys := prepareBenchmarkStore(b, entries, entries)
			largeValue := strings.Repeat("x", int(store.maxBytes)-len("large"))
			if err := store.Set("large", largeValue); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, key := range keys {
					if err := store.Set(key, benchmarkValue); err != nil {
						b.Fatal(err)
					}
				}
				if err := store.Set("large", largeValue); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(entries+1), "writes/op")
		})
	}
}
