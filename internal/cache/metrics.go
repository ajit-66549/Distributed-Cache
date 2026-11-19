package cache

import (
	"log"

	"github.com/ajit-66549/distributed-cache/internal/metrics"
)

// publishSizeMetricsLocked publishes a complete physical-size snapshot. The
// caller must hold s.mu, or be constructing a store that has not been published.
// Call after a full mutation batch, never between its individual removals.
func (s *Store) publishSizeMetricsLocked() {
	if s.metrics == nil {
		return
	}
	if err := s.metrics.SetCacheStats(metrics.CacheStats{
		UsedBytes: s.usedBytes,
		MaxBytes:  s.maxBytes,
		Items:     len(s.entries),
	}); err != nil {
		// Invalid statistics indicate an accounting invariant violation. Reporting
		// them must not change the result of an already-applied durable mutation.
		log.Printf("publish cache size metrics: %v", err)
	}
}
