package cache

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestCalculateEntrySize(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		value      string
		wantedSize int64
	}{
		{
			name:       "key and value",
			key:        "name",
			value:      "Ajit",
			wantedSize: 8,
		},
		{
			name:       "empty value",
			key:        "name",
			value:      "",
			wantedSize: 4,
		},
		{
			name:       "empty key and value",
			key:        "",
			value:      "",
			wantedSize: 0,
		},
		{
			name:       "UTF-8 bytes",
			key:        "é",
			value:      "🙂",
			wantedSize: 6,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			size := calculateEntrySize(test.key, test.value)

			if size != test.wantedSize {
				t.Errorf(
					"expected size %d, got %d",
					test.wantedSize,
					size,
				)
			}
		})
	}
}

func newLimitedStore(tb testing.TB, maxBytes int64) *Store {
	tb.Helper()
	store, err := NewStoreWithLimit(maxBytes)
	if err != nil {
		tb.Fatal(err)
	}
	return store
}

// Check snapshots under the lock, without Get calls that would change LRU order.
func storeInvariantError(store *Store) error {
	store.mu.RLock()
	defer store.mu.RUnlock()

	if len(store.entries) != store.lru.Len() {
		return fmt.Errorf("map has %d entries, list has %d", len(store.entries), store.lru.Len())
	}
	var total int64
	for element := store.lru.Front(); element != nil; element = element.Next() {
		item := element.Value.(*lruItem)
		if store.entries[item.Key] != element {
			return fmt.Errorf("map and list disagree for key %q", item.Key)
		}
		size := int64(len(item.Key)) + int64(len(item.Entry.Value))
		if item.Size != size {
			return fmt.Errorf("key %q has size %d, want %d", item.Key, item.Size, size)
		}
		total += size
	}
	if store.usedBytes != total {
		return fmt.Errorf("usedBytes is %d, entry sizes total %d", store.usedBytes, total)
	}
	if store.usedBytes < 0 || store.usedBytes > store.maxBytes {
		return fmt.Errorf("usedBytes %d is outside budget [0, %d]", store.usedBytes, store.maxBytes)
	}
	return nil
}

func assertStoreState(t *testing.T, store *Store, wantKeys []string, wantBytes int64) {
	t.Helper()
	if err := storeInvariantError(store); err != nil {
		t.Fatal(err)
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	var keys []string
	for element := store.lru.Front(); element != nil; element = element.Next() {
		keys = append(keys, element.Value.(*lruItem).Key)
	}
	if !slices.Equal(keys, wantKeys) {
		t.Errorf("LRU order is %v, want %v (most to least recent)", keys, wantKeys)
	}
	if store.usedBytes != wantBytes {
		t.Errorf("usedBytes is %d, want %d", store.usedBytes, wantBytes)
	}
}

func mustSet(t *testing.T, store *Store, key, value string) {
	t.Helper()
	if err := store.Set(key, value); err != nil {
		t.Fatal(err)
	}
	if err := storeInvariantError(store); err != nil {
		t.Fatal(err)
	}
}

func TestNewStoreRejectsNonPositiveLimits(t *testing.T) {
	for _, limit := range []int64{0, -1} {
		store, err := NewStoreWithLimit(limit)
		if err == nil || store != nil {
			t.Errorf("NewStoreWithLimit(%d) = (%v, %v), want nil store and error", limit, store, err)
		}
	}
}

func TestLRUEvictionOrder(t *testing.T) {
	for _, access := range []string{"none", "read", "update", "TTL update", "missing read"} {
		t.Run(access, func(t *testing.T) {
			store := newLimitedStore(t, 12)
			for _, key := range []string{"a", "b", "c"} {
				mustSet(t, store, key, "xxx") // Four bytes per entry.
			}
			assertStoreState(t, store, []string{"c", "b", "a"}, 12)

			wantKeys := []string{"d", "c", "b"}
			switch access {
			case "read":
				if value, found := store.Get("a"); !found || value != "xxx" {
					t.Fatal("expected a cache hit for a")
				}
				wantKeys = []string{"d", "a", "c"}
			case "update":
				mustSet(t, store, "a", "yyy")
				wantKeys = []string{"d", "a", "c"}
			case "TTL update":
				if err := store.SetWithTTL("a", "yyy", time.Hour); err != nil {
					t.Fatal(err)
				}
				wantKeys = []string{"d", "a", "c"}
			case "missing read":
				if _, found := store.Get("missing"); found {
					t.Fatal("unexpected cache hit")
				}
			}
			mustSet(t, store, "d", "xxx")
			assertStoreState(t, store, wantKeys, 12)
		})
	}
}

func TestLRUEvictsMultipleEntries(t *testing.T) {
	store := newLimitedStore(t, 12)
	for _, key := range []string{"a", "b", "c"} {
		mustSet(t, store, key, "xxx")
	}
	// Six incoming bytes require evicting a and b, leaving c.
	mustSet(t, store, "d", "xxxxx")
	assertStoreState(t, store, []string{"d", "c"}, 10)
}

func TestLRUOverwriteAndDeleteAccounting(t *testing.T) {
	store := newLimitedStore(t, 12)
	for _, key := range []string{"a", "b", "c"} {
		mustSet(t, store, key, "xxx")
	}

	// Growing the least recent entry protects it and evicts b.
	mustSet(t, store, "a", "xxxxxxx")
	assertStoreState(t, store, []string{"a", "c"}, 12)

	// Shrinking a releases space, allowing b to fit exactly without eviction.
	mustSet(t, store, "a", "x")
	assertStoreState(t, store, []string{"a", "c"}, 6)
	mustSet(t, store, "b", "xxxxx")
	assertStoreState(t, store, []string{"b", "a", "c"}, 12)
	mustSet(t, store, "a", "y")
	assertStoreState(t, store, []string{"a", "b", "c"}, 12)

	if !store.Delete("b") || store.Delete("b") {
		t.Fatal("expected exactly one successful deletion of b")
	}
	assertStoreState(t, store, []string{"a", "c"}, 6)
	store.Delete("a")
	store.Delete("c")
	assertStoreState(t, store, nil, 0)
}

func TestLRUExactBudgetAndZeroSizeEntry(t *testing.T) {
	store := newLimitedStore(t, 4)
	mustSet(t, store, "", "")
	mustSet(t, store, "a", "xxx")
	assertStoreState(t, store, []string{"a", ""}, 4)

	// A zero-sized entry can grow to fill the entire budget.
	mustSet(t, store, "", "1234")
	assertStoreState(t, store, []string{""}, 4)
	mustSet(t, store, "", "")
	assertStoreState(t, store, []string{""}, 0)
	mustSet(t, store, "b", "xxx")
	mustSet(t, store, "c", "xxx")
	assertStoreState(t, store, []string{"c"}, 4)
}

func TestLRURejectsOversizedWrites(t *testing.T) {
	for _, withTTL := range []bool{false, true} {
		for _, key := range []string{"c", "a", "long-key!"} {
			t.Run(fmt.Sprintf("TTL=%t/key=%s", withTTL, key), func(t *testing.T) {
				store := newLimitedStore(t, 8)
				if err := store.SetWithTTL("a", "xxx", time.Hour); err != nil {
					t.Fatal(err)
				}
				mustSet(t, store, "b", "xxx")
				original := store.entries["a"].Value.(*lruItem).Entry

				value := "xxxxxxxx"
				if key == "long-key!" {
					value = "" // The key alone exceeds the budget.
				}
				var err error
				if withTTL {
					err = store.SetWithTTL(key, value, 2*time.Hour)
				} else {
					err = store.Set(key, value)
				}
				if !errors.Is(err, ErrEntryTooLarge) {
					t.Fatalf("expected ErrEntryTooLarge, got %v", err)
				}
				assertStoreState(t, store, []string{"b", "a"}, 8)
				if got := store.entries["a"].Value.(*lruItem).Entry; got != original {
					t.Fatalf("rejected write changed old value or expiration: got %+v, want %+v", got, original)
				}
			})
		}
	}
}

func TestLRUExpirationAndEviction(t *testing.T) {
	t.Run("lazy expiration frees space", func(t *testing.T) {
		store := newLimitedStore(t, 8)
		// Already elapsed TTLs make these cases deterministic without sleeps.
		if err := store.SetWithTTL("a", "xxx", -time.Second); err != nil {
			t.Fatal(err)
		}
		mustSet(t, store, "b", "xxx")
		if _, found := store.Get("a"); found {
			t.Fatal("expired entry was returned")
		}
		assertStoreState(t, store, []string{"b"}, 4)
		mustSet(t, store, "c", "xxx")
		if deleted := store.DeleteExpired(); deleted != 0 {
			t.Fatalf("already removed entry was cleaned again: %d", deleted)
		}
		assertStoreState(t, store, []string{"c", "b"}, 8)
	})

	t.Run("cleanup before eviction", func(t *testing.T) {
		store := newLimitedStore(t, 12)
		if err := store.SetWithTTL("a", "xxx", -time.Second); err != nil {
			t.Fatal(err)
		}
		mustSet(t, store, "b", "xxx")
		if err := store.SetWithTTL("c", "xxx", time.Hour); err != nil {
			t.Fatal(err)
		}
		if deleted := store.DeleteExpired(); deleted != 1 {
			t.Fatalf("expected one expired entry, got %d", deleted)
		}
		assertStoreState(t, store, []string{"c", "b"}, 8)
		mustSet(t, store, "d", "xxxxx")
		assertStoreState(t, store, []string{"d", "c"}, 10)
	})

	t.Run("eviction before cleanup", func(t *testing.T) {
		store := newLimitedStore(t, 8)
		if err := store.SetWithTTL("a", "xxx", -time.Second); err != nil {
			t.Fatal(err)
		}
		mustSet(t, store, "b", "xxx")
		mustSet(t, store, "c", "xxx")
		if deleted := store.DeleteExpired(); deleted != 0 {
			t.Fatalf("evicted entry was cleaned again: %d", deleted)
		}
		assertStoreState(t, store, []string{"c", "b"}, 8)
	})

	t.Run("successful update clears old TTL", func(t *testing.T) {
		store := newLimitedStore(t, 4)
		if err := store.SetWithTTL("a", "xxx", -time.Second); err != nil {
			t.Fatal(err)
		}
		mustSet(t, store, "a", "yyy")
		if value, found := store.Get("a"); !found || value != "yyy" {
			t.Fatal("updated value retained its old expiration")
		}
		assertStoreState(t, store, []string{"a"}, 4)
	})
}
