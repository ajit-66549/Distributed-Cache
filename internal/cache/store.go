package cache

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

// DefaultMaxBytes is the default budget for key and value bytes (64 MiB).
const DefaultMaxBytes int64 = 64 * 1024 * 1024

type Store struct {
	mu sync.RWMutex

	// Finds a list element by key in O(1).
	entries map[string]*list.Element

	// Front = most recently used.
	// Back  = least recently used.
	lru *list.List

	usedBytes int64
	maxBytes  int64
}

func NewStore() *Store {
	return &Store{
		entries:  make(map[string]*list.Element),
		lru:      list.New(),
		maxBytes: DefaultMaxBytes,
	}
}

// NewStoreWithLimit creates a store with a positive byte budget.
func NewStoreWithLimit(maxBytes int64) (*Store, error) {
	if maxBytes <= 0 {
		return nil, errors.New("maximum cache size must be greater than zero")
	}

	store := NewStore()
	store.maxBytes = maxBytes
	return store, nil
}

func (s *Store) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.setLocked(key, Entry{
		Value: value,
	})
}

func (s *Store) SetWithTTL(
	key string,
	value string,
	ttl time.Duration,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.setLocked(key, Entry{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
	})
}

func (s *Store) setLocked(key string, entry Entry) error {
	size := calculateEntrySize(key, entry.Value)
	if size > s.maxBytes {
		return ErrEntryTooLarge
	}

	// The key already exists, so update its list element.
	if element, found := s.entries[key]; found {
		item := element.Value.(*lruItem)

		// Exclude the old value while making room for its replacement.
		s.usedBytes -= item.Size

		// Protect this entry from eviction by making it most recently used.
		s.lru.MoveToFront(element)
		s.evictToFitLocked(size)

		item.Entry = entry
		item.Size = size

		s.usedBytes += item.Size

		return nil
	}

	s.evictToFitLocked(size)

	item := &lruItem{
		Key:   key,
		Entry: entry,
		Size:  size,
	}

	// Add the new item to the front because it was just used.
	element := s.lru.PushFront(item)

	// The map points directly to the list element.
	s.entries[key] = element

	s.usedBytes += item.Size
	return nil
}

func (s *Store) Get(key string) (string, bool) {
	/*
		Get now modifies the linked-list order by moving an entry
		to the front, so it requires a write lock.
	*/
	s.mu.Lock()
	defer s.mu.Unlock()

	element, found := s.entries[key]
	if !found {
		return "", false
	}

	item := element.Value.(*lruItem)

	if item.Entry.IsExpired(time.Now()) {
		s.removeElementLocked(element)
		return "", false
	}

	// A successfully retrieved entry becomes most recently used.
	s.lru.MoveToFront(element)

	return item.Entry.Value, true
}

func (s *Store) Delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	element, found := s.entries[key]
	if !found {
		return false
	}

	s.removeElementLocked(element)

	return true
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.entries)
}

func (s *Store) DeleteExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	deleted := 0

	for _, element := range s.entries {
		item := element.Value.(*lruItem)

		if item.Entry.IsExpired(now) {
			s.removeElementLocked(element)
			deleted++
		}
	}

	return deleted
}

func (s *Store) removeElementLocked(element *list.Element) {
	item := element.Value.(*lruItem)

	delete(s.entries, item.Key)
	s.lru.Remove(element)
	s.usedBytes -= item.Size
}
