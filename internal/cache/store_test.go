package cache

import (
	"testing"
	"time"
)

func TestSetAndGet(t *testing.T) {
	store := NewStore()

	store.Set("name", "Ajit")

	value, found := store.Get("name")

	if !found {
		t.Fatal("expected key to be found")
	}

	if value != "Ajit" {
		t.Errorf("Expected Ajit, got %s", value)
	}
}

func TestGetMissingKey(t *testing.T) {
	store := NewStore()

	value, found := store.Get("missing")

	if found {
		t.Fatal("expected key to be missing")
	}

	if value != "" {
		t.Errorf("Expected empty value, got %s", value)
	}
}

func TestDeleteExistingKey(t *testing.T) {
	store := NewStore()
	store.Set("name", "Ajit")

	deleted := store.Delete("name")

	if !deleted {
		t.Fatal("expected key to be deleted")
	}

	_, found := store.Get("name")

	if found {
		t.Errorf("expected key to be missing after deletion")
	}

	if store.Len() != 0 {
		t.Errorf("expected store length 0, got %d", store.Len())
	}
}

func TestDeleteMissingKey(t *testing.T) {
	store := NewStore()

	deleted := store.Delete("missing")

	if deleted {
		t.Fatal("expected missing key not to be deleted")
	}

	if store.Len() != 0 {
		t.Errorf("expected store length 0, got %d", store.Len())
	}
}

func TestSetOverwritesExistingKey(t *testing.T) {
	store := NewStore()

	store.Set("name", "Ajit")
	store.Set("name", "Ram")

	value, found := store.Get("name")

	if !found {
		t.Fatal("expected key to be found")
	}

	if value != "Ram" {
		t.Errorf("expected Ram, got %s", value)
	}

	if store.Len() != 1 {
		t.Errorf("expected store length 1, got %d", store.Len())
	}
}

func TestSetEmptyValue(t *testing.T) {
	store := NewStore()

	store.Set("message", "")

	value, found := store.Get("message")

	if !found {
		t.Fatal("expected key with empty value to be found")
	}

	if value != "" {
		t.Errorf("expected empty value, got %s", value)
	}
}

func TestLenWithMultipleEntries(t *testing.T) {
	store := NewStore()

	store.Set("name", "Ajit")
	store.Set("city", "Austin")

	if store.Len() != 2 {
		t.Errorf("expected store length 2, got %d", store.Len())
	}
}

func TestGetExpiredEntry(t *testing.T) {
	store := NewStore()
	store.SetWithTTL("name", "Ajit", 10*time.Millisecond)

	time.Sleep(20 * time.Millisecond)

	value, found := store.Get("name")

	if found {
		t.Fatal("expected expired key to be missing")
	}

	if value != "" {
		t.Errorf("expected empty value, got %q", value)
	}

	if store.Len() != 0 {
		t.Errorf("expected expired entry to be deleted, got length %d", store.Len())
	}
}

func TestDeleteExpired(t *testing.T) {
	store := NewStore()

	store.Set("permanent", "value")
	store.SetWithTTL("expired", "value", 10*time.Millisecond)
	store.SetWithTTL("active", "value", time.Hour)

	time.Sleep(20 * time.Millisecond)

	deleted := store.DeleteExpired()

	if deleted != 1 {
		t.Errorf("expected 1 deleted entry, got %d", deleted)
	}

	if store.Len() != 2 {
		t.Errorf("expected store length 2, got %d", store.Len())
	}

	if _, found := store.Get("expired"); found {
		t.Error("expected expired entry to be missing")
	}

	if _, found := store.Get("permanent"); !found {
		t.Error("expected permanent entry to remain")
	}

	if _, found := store.Get("active"); !found {
		t.Error("expected active entry to remain")
	}
}
