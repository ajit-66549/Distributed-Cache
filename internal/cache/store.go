package cache

type Store struct {
	entries map[string]Entry
}

func NewStore() *Store {
	return &Store{
		entries: make(map[string]Entry),
	}
}

func (s *Store) Set(key string, value string) {
	s.entries[key] = Entry{Value: value}
}

func (s *Store) Get(key string) (string, bool) {
	entry, found := s.entries[key]

	if !found {
		return "", false
	}

	return entry.Value, true
}

func (s *Store) Delete(key string) bool {
	_, found := s.entries[key]

	if !found {
		return false
	}

	delete(s.entries, key)
	return true
}

func (s *Store) Len() int {
	return len(s.entries)
}
