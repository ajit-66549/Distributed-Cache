package cache

import "errors"

// ErrEntryTooLarge means the key and value together exceed the cache's budget.
var ErrEntryTooLarge = errors.New("entry exceeds cache memory budget")

type lruItem struct {
	Key   string
	Entry Entry
	Size  int64
}

func calculateEntrySize(key, value string) int64 {
	return int64(len(key)) + int64(len(value))
}

// evictToFitLocked makes room for size bytes. The caller must hold s.mu
// and ensure size does not exceed s.maxBytes.
func (s *Store) evictToFitLocked(size int64) {
	// Subtract before comparing to avoid overflowing usedBytes + size.
	for s.usedBytes > s.maxBytes-size {
		s.removeElementLocked(s.lru.Back())
	}
}
