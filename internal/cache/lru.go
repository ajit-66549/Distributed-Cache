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

// planEvictionsLocked selects victims in LRU order without changing the cache.
// size includes the key and replacement value; an existing key is protected.
// The caller must hold s.mu until the plan is applied or discarded.
func (s *Store) planEvictionsLocked(key string, size int64) ([]string, error) {
	if size > s.maxBytes {
		return nil, ErrEntryTooLarge
	}

	usedBytes := s.usedBytes
	if element, found := s.entries[key]; found {
		usedBytes -= element.Value.(*lruItem).Size
	}

	var victims []string
	for element := s.lru.Back(); element != nil && usedBytes > s.maxBytes-size; element = element.Prev() {
		item := element.Value.(*lruItem)
		if item.Key == key {
			continue
		}
		victims = append(victims, item.Key)
		usedBytes -= item.Size
	}
	return victims, nil
}

// evictToFitLocked makes room for size bytes. The caller must hold s.mu
// and ensure size does not exceed s.maxBytes.
func (s *Store) evictToFitLocked(size int64) {
	// Subtract before comparing to avoid overflowing usedBytes + size.
	for s.usedBytes > s.maxBytes-size {
		s.removeElementLocked(s.lru.Back())
	}
}
