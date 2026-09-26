package cache

import (
	"context"
	"testing"
	"time"
)

func TestExpirationCleanup(t *testing.T) {
	store := NewStore()

	store.Set("permanent", "value")
	store.SetWithTTL("temporary", "value", 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartExpirationCleanup(ctx, store, 5*time.Millisecond)

	time.Sleep(30 * time.Millisecond)

	if _, found := store.Get("temporary"); found {
		t.Fatal("expected expired entry to be removed")
	}

	if _, found := store.Get("permanent"); !found {
		t.Fatal("expected permanent entry to remain")
	}

	if store.Len() != 1 {
		t.Errorf("expected store length 1, got %d", store.Len())
	}
}
