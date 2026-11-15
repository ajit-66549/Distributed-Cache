// Package metrics records the cache's process-local measurements. It does not
// inspect cache entries, instrument operations, or install HTTP routes.
package metrics

import (
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics owns an independent registry. Use New to construct it; the zero value
// is not usable. All methods may be called concurrently. Do not copy Metrics.
type Metrics struct {
	registry           *prometheus.Registry
	hits               prometheus.Counter
	misses             prometheus.Counter
	evictions          prometheus.Counter
	lazyExpirations    prometheus.Counter
	cleanupExpirations prometheus.Counter
	requests           *prometheus.CounterVec
	requestDuration    *prometheus.HistogramVec
	cacheStats         *cacheStatsCollector
}

// New creates zeroed counters and cache gauges without using the global registry.
// HTTP series appear on their first observation. Heap usage is sampled when
// metrics are gathered, and covers the whole Go process.
func New() *Metrics {
	expirations := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cache",
		Name:      "expirations_total",
		Help:      "Entries physically removed because their TTL expired.",
	}, []string{"source"})
	m := &Metrics{
		registry: prometheus.NewPedanticRegistry(),
		hits: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "cache",
			Name:      "hits_total",
			Help:      "GET operations returning an unexpired entry.",
		}),
		misses: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "cache",
			Name:      "misses_total",
			Help:      "GET operations finding no entry or an expired entry.",
		}),
		evictions: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "cache",
			Name:      "evictions_total",
			Help:      "Entries physically removed by successful LRU eviction plans.",
		}),
		lazyExpirations:    expirations.WithLabelValues("lazy"),
		cleanupExpirations: expirations.WithLabelValues("cleanup"),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "cache",
			Name:      "http_requests_total",
			Help:      "Application HTTP requests by method, matched route, and response status.",
		}, []string{"method", "route", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "cache",
			Name:      "http_request_duration_seconds",
			Help:      "Application HTTP request duration in seconds, including lock waits and durable writes.",
			Buckets: []float64{
				0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05,
				0.1, 0.25, 0.5, 1, 2.5, 5, 10,
			},
		}, []string{"method", "route"}),
		cacheStats: newCacheStatsCollector(),
	}
	m.registry.MustRegister(
		m.hits, m.misses, m.evictions, expirations,
		m.requests, m.requestDuration, m.cacheStats,
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: "cache",
			Name:      "go_heap_alloc_bytes",
			Help:      "Currently allocated Go heap bytes for the whole process, not cache payload bytes or resident memory.",
		}, func() float64 {
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			return float64(stats.Alloc)
		}),
	)
	return m
}

// Gatherer provides collection access for tests and a future HTTP exporter.
func (m *Metrics) Gatherer() prometheus.Gatherer {
	return m.registry
}

// RecordHit records one GET returning an unexpired value, including an empty one.
func (m *Metrics) RecordHit() {
	m.hits.Inc()
}

// RecordMiss records one GET finding no usable value.
func (m *Metrics) RecordMiss() {
	m.misses.Inc()
}

// AddEvictions records removals after a successful eviction batch has applied.
func (m *Metrics) AddEvictions(count uint64) {
	m.evictions.Add(float64(count))
}

// RecordLazyExpiration records one expired entry physically removed by GET.
// The caller separately records the GET miss.
func (m *Metrics) RecordLazyExpiration() {
	m.lazyExpirations.Inc()
}

// AddCleanupExpirations records entries physically removed by runtime cleanup.
// Recovery and startup cleanup must not call it.
func (m *Metrics) AddCleanupExpirations(count uint64) {
	m.cleanupExpirations.Add(float64(count))
}

// ObserveRequest records one completed HTTP request and its elapsed duration.
// route must be a matched path template, without a method prefix. Unknown methods
// and routes use bounded fallback labels. A status of zero represents an abort
// before a final response; other statuses outside 200..599 also map to aborted.
// Negative durations are clamped to zero. Probe and profiling routes are ignored.
// The HTTP layer is responsible for determining the final status and timing.
func (m *Metrics) ObserveRequest(method, route string, status int, elapsed time.Duration) {
	switch route {
	case "/metrics", "/healthz", "/readyz", "/debug/pprof":
		return
	}
	if strings.HasPrefix(route, "/debug/pprof/") {
		return
	}

	switch method {
	case "GET", "PUT", "DELETE", "HEAD", "OPTIONS":
	default:
		method = "OTHER"
	}
	switch route {
	case "/v1/cache/{key}", "/v1/ping":
	default:
		route = "unmatched"
	}
	statusLabel := "aborted"
	if status >= 200 && status <= 599 {
		statusLabel = strconv.Itoa(status)
	}
	if elapsed < 0 {
		elapsed = 0
	}
	m.requests.WithLabelValues(method, route, statusLabel).Inc()
	m.requestDuration.WithLabelValues(method, route).Observe(elapsed.Seconds())
}
