package cache

import (
	"fmt"
	"sync"
	"testing"
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

			if !store.Delete(key) {
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
