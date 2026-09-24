package cache

import "sync"

type Store struct {
	mu      sync.RWMutex
	entries map[string]Entry
}

func NewStore() *Store {
	return &Store{
		entries: make(map[string]Entry),
	}
}

func (s *Store) Set(key string, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = Entry{Value: value}
}

func (s *Store) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, found := s.entries[key]

	if !found {
		return "", false
	}

	return entry.Value, true
}

func (s *Store) Delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, found := s.entries[key]

	if !found {
		return false
	}

	delete(s.entries, key)
	return true
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.entries)
}
