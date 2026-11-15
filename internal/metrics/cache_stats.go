package metrics

import (
	"errors"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// CacheStats describes physical storage, including expired entries awaiting
// removal. It counts key/value bytes, excluding cache and runtime overhead.
type CacheStats struct {
	UsedBytes int64
	MaxBytes  int64
	Items     int
}

// SetCacheStats publishes a complete snapshot of the cache gauges.
// The caller must capture and publish it while holding the cache lock so an older
// state cannot overwrite a newer one. Collection does not acquire cache locks.
// Invalid input leaves the previous snapshot unchanged.
func (m *Metrics) SetCacheStats(stats CacheStats) error {
	if stats.UsedBytes < 0 || stats.MaxBytes < 0 || stats.Items < 0 || stats.UsedBytes > stats.MaxBytes {
		return errors.New("cache statistics must be nonnegative and used bytes must not exceed max bytes")
	}
	m.cacheStats.mu.Lock()
	m.cacheStats.snapshot = stats
	m.cacheStats.mu.Unlock()
	return nil
}

type cacheStatsCollector struct {
	mu       sync.RWMutex
	snapshot CacheStats
	used     *prometheus.Desc
	max      *prometheus.Desc
	items    *prometheus.Desc
}

func newCacheStatsCollector() *cacheStatsCollector {
	return &cacheStatsCollector{
		used: prometheus.NewDesc("cache_used_bytes",
			"Key and value bytes physically stored, including expired entries awaiting removal.", nil, nil),
		max: prometheus.NewDesc("cache_max_bytes",
			"Configured budget for stored key and value bytes.", nil, nil),
		items: prometheus.NewDesc("cache_items",
			"Physically stored entries, including expired entries awaiting removal.", nil, nil),
	}
}

func (c *cacheStatsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.used
	ch <- c.max
	ch <- c.items
}

func (c *cacheStatsCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	stats := c.snapshot
	c.mu.RUnlock()
	ch <- prometheus.MustNewConstMetric(c.used, prometheus.GaugeValue, float64(stats.UsedBytes))
	ch <- prometheus.MustNewConstMetric(c.max, prometheus.GaugeValue, float64(stats.MaxBytes))
	ch <- prometheus.MustNewConstMetric(c.items, prometheus.GaugeValue, float64(stats.Items))
}
