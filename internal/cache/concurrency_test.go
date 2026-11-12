package cache

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoreConcurrentAccess(t *testing.T) {
	store := NewStore()

	const operations = 100

	// Prepare stable keys for readers and keys for deletion.
	for i := 0; i < operations; i++ {
		store.Set(fmt.Sprintf("read-%d", i), "stable")
		store.Set(fmt.Sprintf("delete-%d", i), "temporary")
	}

	var wg sync.WaitGroup

	for i := 0; i < operations; i++ {
		i := i

		wg.Add(3)

		// Writer
		go func() {
			defer wg.Done()

			key := fmt.Sprintf("write-%d", i)
			store.Set(key, "new")
		}()

		// Reader
		go func() {
			defer wg.Done()

			key := fmt.Sprintf("read-%d", i)
			value, found := store.Get(key)

			if !found || value != "stable" {
				t.Errorf("expected %s to contain stable", key)
			}
		}()

		// Deleter
		go func() {
			defer wg.Done()

			key := fmt.Sprintf("delete-%d", i)

			deleted, err := store.Delete(key)
			if err != nil {
				t.Errorf("delete %s: %v", key, err)
				return
			}
			if !deleted {
				t.Errorf("expected %s to be deleted", key)
			}
		}()
	}

	wg.Wait()

	// 100 read keys remain + 100 newly written keys.
	if store.Len() != operations*2 {
		t.Errorf(
			"expected store length %d, got %d",
			operations*2,
			store.Len(),
		)
	}
}

func TestStoreConcurrentEvictionAndExpiration(t *testing.T) {
	store := newLimitedStore(t, 96)
	for i := 0; i < 8; i++ {
		mustSet(t, store, fmt.Sprintf("key-%02d", i), strings.Repeat("x", 24))
	}

	const workers = 8
	failures := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for operation := 0; operation < 200; operation++ {
				key := fmt.Sprintf("key-%02d", (worker+operation)%16)
				value := strings.Repeat("x", 1+(worker+operation)%24)
				var err error
				switch (worker + operation) % 6 {
				case 0:
					err = store.Set(key, value)
				case 1:
					err = store.SetWithTTL(key, value, time.Hour)
				case 2:
					err = store.SetWithTTL(key, value, -time.Second)
				case 3:
					store.Get(key)
				case 4:
					store.Delete(key)
				case 5:
					store.DeleteExpired()
				}
				if err == nil {
					err = storeInvariantError(store)
				}
				if err != nil {
					failures <- fmt.Errorf("worker %d, operation %d: %w", worker, operation, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if err := storeInvariantError(store); err != nil {
		t.Fatal(err)
	}
}
