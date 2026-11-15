package metrics_test

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/metrics"
	dto "github.com/prometheus/client_model/go"
)

func gather(t *testing.T, m *metrics.Metrics) map[string]*dto.MetricFamily {
	t.Helper()
	families, err := m.Gatherer().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	result := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		result[family.GetName()] = family
	}
	return result
}

func findMetric(t *testing.T, family *dto.MetricFamily, labels map[string]string) *dto.Metric {
	t.Helper()
	if family == nil {
		t.Fatal("missing metric family")
	}
	for _, metric := range family.GetMetric() {
		if len(metric.GetLabel()) != len(labels) {
			continue
		}
		matched := true
		for _, label := range metric.GetLabel() {
			if value, ok := labels[label.GetName()]; !ok || value != label.GetValue() {
				matched = false
				break
			}
		}
		if matched {
			return metric
		}
	}
	t.Fatalf("metric %s with labels %v not found", family.GetName(), labels)
	return nil
}

func counter(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string) float64 {
	t.Helper()
	metric := findMetric(t, families[name], labels)
	if metric.Counter == nil {
		t.Fatalf("%s is not a counter", name)
	}
	return metric.GetCounter().GetValue()
}

func gauge(t *testing.T, families map[string]*dto.MetricFamily, name string) float64 {
	t.Helper()
	metric := findMetric(t, families[name], nil)
	if metric.Gauge == nil {
		t.Fatalf("%s is not a gauge", name)
	}
	return metric.GetGauge().GetValue()
}

func TestNewExportsContractMetrics(t *testing.T) {
	m := metrics.New()
	initial := gather(t, m)
	if len(initial) != 8 {
		t.Fatalf("initial metric families = %d, want 8", len(initial))
	}
	for _, name := range []string{"cache_hits_total", "cache_misses_total", "cache_evictions_total"} {
		if got := counter(t, initial, name, nil); got != 0 {
			t.Errorf("initial %s = %g, want 0", name, got)
		}
	}
	for _, source := range []string{"lazy", "cleanup"} {
		if got := counter(t, initial, "cache_expirations_total", map[string]string{"source": source}); got != 0 {
			t.Errorf("initial expirations for %s = %g, want 0", source, got)
		}
	}
	for _, name := range []string{"cache_used_bytes", "cache_max_bytes", "cache_items"} {
		if got := gauge(t, initial, name); got != 0 {
			t.Errorf("initial %s = %g, want 0", name, got)
		}
	}
	if got := gauge(t, initial, "cache_go_heap_alloc_bytes"); got <= 0 {
		t.Errorf("process heap allocation = %g, want a positive runtime sample", got)
	}

	m.ObserveRequest("GET", "/v1/cache/{key}", 200, time.Millisecond)
	families := gather(t, m)
	wantTypes := map[string]dto.MetricType{
		"cache_hits_total":                    dto.MetricType_COUNTER,
		"cache_misses_total":                  dto.MetricType_COUNTER,
		"cache_evictions_total":               dto.MetricType_COUNTER,
		"cache_expirations_total":             dto.MetricType_COUNTER,
		"cache_http_requests_total":           dto.MetricType_COUNTER,
		"cache_http_request_duration_seconds": dto.MetricType_HISTOGRAM,
		"cache_used_bytes":                    dto.MetricType_GAUGE,
		"cache_max_bytes":                     dto.MetricType_GAUGE,
		"cache_items":                         dto.MetricType_GAUGE,
		"cache_go_heap_alloc_bytes":           dto.MetricType_GAUGE,
	}
	if len(families) != len(wantTypes) {
		t.Fatalf("metric families = %d, want %d", len(families), len(wantTypes))
	}
	for name, want := range wantTypes {
		family := families[name]
		if family == nil || family.GetType() != want || family.GetHelp() == "" {
			t.Errorf("invalid definition for %s: %v", name, family)
		}
	}
}

func TestEngineEventsAndReadOnlyCollection(t *testing.T) {
	m := metrics.New()
	m.RecordHit()
	m.RecordHit()
	m.RecordMiss()
	m.AddEvictions(3)
	m.AddEvictions(0)
	m.RecordLazyExpiration()
	m.AddCleanupExpirations(2)
	m.AddCleanupExpirations(0)
	stats := metrics.CacheStats{UsedBytes: 8, MaxBytes: 64, Items: 1}
	if err := m.SetCacheStats(stats); err != nil {
		t.Fatal(err)
	}
	stats.UsedBytes = 999 // The published snapshot must not alias caller storage.

	for range 3 {
		families := gather(t, m)
		for name, want := range map[string]float64{
			"cache_hits_total": 2, "cache_misses_total": 1, "cache_evictions_total": 3,
		} {
			if got := counter(t, families, name, nil); got != want {
				t.Errorf("%s = %g, want %g", name, got, want)
			}
		}
		for source, want := range map[string]float64{"lazy": 1, "cleanup": 2} {
			if got := counter(t, families, "cache_expirations_total", map[string]string{"source": source}); got != want {
				t.Errorf("%s expirations = %g, want %g", source, got, want)
			}
		}
		for name, want := range map[string]float64{
			"cache_used_bytes": 8, "cache_max_bytes": 64, "cache_items": 1,
		} {
			if got := gauge(t, families, name); got != want {
				t.Errorf("%s = %g, want %g", name, got, want)
			}
		}
		if families["cache_http_requests_total"] != nil {
			t.Error("engine events created HTTP request measurements")
		}
	}
}

func TestCacheStatsReplaceAndRejectInvalidSnapshots(t *testing.T) {
	m := metrics.New()
	for _, stats := range []metrics.CacheStats{
		{UsedBytes: 8, MaxBytes: 64, Items: 1},
		{UsedBytes: 16, MaxBytes: 64, Items: 2},
		{UsedBytes: 4, MaxBytes: 64, Items: 1},
		{MaxBytes: 64},
	} {
		if err := m.SetCacheStats(stats); err != nil {
			t.Fatal(err)
		}
		families := gather(t, m)
		if gauge(t, families, "cache_used_bytes") != float64(stats.UsedBytes) ||
			gauge(t, families, "cache_max_bytes") != float64(stats.MaxBytes) ||
			gauge(t, families, "cache_items") != float64(stats.Items) {
			t.Fatalf("gauges do not reflect snapshot %+v", stats)
		}
	}
	for _, invalid := range []metrics.CacheStats{
		{UsedBytes: -1, MaxBytes: 64},
		{MaxBytes: -1},
		{MaxBytes: 64, Items: -1},
		{UsedBytes: 65, MaxBytes: 64},
		{UsedBytes: 1},
	} {
		if err := m.SetCacheStats(invalid); err == nil {
			t.Errorf("accepted invalid snapshot %+v", invalid)
		}
		families := gather(t, m)
		if gauge(t, families, "cache_used_bytes") != 0 ||
			gauge(t, families, "cache_max_bytes") != 64 ||
			gauge(t, families, "cache_items") != 0 {
			t.Fatal("invalid snapshot changed the previous gauges")
		}
	}
}

func TestRequestLabelsAndStatuses(t *testing.T) {
	m := metrics.New()
	for _, request := range []struct {
		method string
		route  string
		status int
	}{
		{"GET", "/v1/cache/{key}", 200},
		{"GET", "/v1/cache/{key}", 404},
		{"PUT", "/v1/cache/{key}", 201},
		{"PUT", "/v1/cache/{key}", 400},
		{"PUT", "/v1/cache/{key}", 413},
		{"DELETE", "/v1/cache/{key}", 500},
		{"HEAD", "/v1/ping", 200},
		{"OPTIONS", "unmatched", 405},
	} {
		m.ObserveRequest(request.method, request.route, request.status, time.Millisecond)
	}
	m.ObserveRequest("GET", "/v1/cache/{key}", 0, time.Millisecond)
	families := gather(t, m)
	for _, labels := range []map[string]string{
		{"method": "GET", "route": "/v1/cache/{key}", "status": "200"},
		{"method": "GET", "route": "/v1/cache/{key}", "status": "404"},
		{"method": "GET", "route": "/v1/cache/{key}", "status": "aborted"},
		{"method": "PUT", "route": "/v1/cache/{key}", "status": "201"},
		{"method": "PUT", "route": "/v1/cache/{key}", "status": "400"},
		{"method": "PUT", "route": "/v1/cache/{key}", "status": "413"},
		{"method": "DELETE", "route": "/v1/cache/{key}", "status": "500"},
		{"method": "HEAD", "route": "/v1/ping", "status": "200"},
		{"method": "OPTIONS", "route": "unmatched", "status": "405"},
	} {
		if got := counter(t, families, "cache_http_requests_total", labels); got != 1 {
			t.Errorf("requests %v = %g, want 1", labels, got)
		}
	}
	if got := counter(t, families, "cache_hits_total", nil); got != 0 {
		t.Errorf("HTTP observations changed engine hits to %g", got)
	}
}

func TestRequestLabelsRemainBounded(t *testing.T) {
	m := metrics.New()
	for i := range 100 {
		m.ObserveRequest(fmt.Sprintf("CUSTOM-%d", i), fmt.Sprintf("/v1/cache/secret-%d?token=x", i),
			1000+i, -time.Second)
	}
	m.ObserveRequest("POST", "/another/raw/path", 100, 0)
	families := gather(t, m)
	requests := families["cache_http_requests_total"]
	if len(requests.GetMetric()) != 1 {
		t.Fatalf("untrusted inputs created %d request series, want 1", len(requests.GetMetric()))
	}
	labels := map[string]string{"method": "OTHER", "route": "unmatched", "status": "aborted"}
	if got := counter(t, families, "cache_http_requests_total", labels); got != 101 {
		t.Errorf("fallback requests = %g, want 101", got)
	}
	durations := families["cache_http_request_duration_seconds"]
	if len(durations.GetMetric()) != 1 {
		t.Fatalf("untrusted inputs created %d duration series, want 1", len(durations.GetMetric()))
	}
	histogram := findMetric(t, durations, map[string]string{"method": "OTHER", "route": "unmatched"}).GetHistogram()
	if histogram.GetSampleCount() != 101 || histogram.GetSampleSum() != 0 {
		t.Errorf("invalid durations were not clamped: %v", histogram)
	}
}

func TestProbesAndProfilingAreExcluded(t *testing.T) {
	m := metrics.New()
	for _, route := range []string{
		"/metrics", "/healthz", "/readyz", "/debug/pprof", "/debug/pprof/", "/debug/pprof/profile",
	} {
		m.ObserveRequest("GET", route, 200, time.Second)
	}
	families := gather(t, m)
	if families["cache_http_requests_total"] != nil || families["cache_http_request_duration_seconds"] != nil {
		t.Fatal("probes or profiling created application request measurements")
	}
}

func TestDurationHistogramBoundariesAndOverflow(t *testing.T) {
	m := metrics.New()
	for _, elapsed := range []time.Duration{0, 100 * time.Microsecond, 500 * time.Microsecond, time.Millisecond, 11 * time.Second} {
		m.ObserveRequest("GET", "/v1/cache/{key}", 200, elapsed)
	}
	families := gather(t, m)
	histogram := findMetric(t, families["cache_http_request_duration_seconds"],
		map[string]string{"method": "GET", "route": "/v1/cache/{key}"}).GetHistogram()
	if histogram.GetSampleCount() != 5 || math.Abs(histogram.GetSampleSum()-11.0016) > 1e-12 {
		t.Fatalf("incorrect sample count or duration units: %v", histogram)
	}
	wantBounds := []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	wantCounts := []uint64{2, 3, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4}
	if len(histogram.GetBucket()) != len(wantBounds) {
		t.Fatalf("finite bucket count = %d, want %d", len(histogram.GetBucket()), len(wantBounds))
	}
	for i, bucket := range histogram.GetBucket() {
		if bucket.GetUpperBound() != wantBounds[i] || bucket.GetCumulativeCount() != wantCounts[i] {
			t.Errorf("bucket %d = %v, want upper bound %g and count %d", i, bucket, wantBounds[i], wantCounts[i])
		}
	}
	// The sample beyond 10 seconds is retained in count/sum and the exporter's +Inf bucket.
	if got := counter(t, families, "cache_http_requests_total",
		map[string]string{"method": "GET", "route": "/v1/cache/{key}", "status": "200"}); got != 5 {
		t.Errorf("requests = %g, want 5 histogram observations", got)
	}
}

func TestInstancesAreIndependent(t *testing.T) {
	first, second := metrics.New(), metrics.New()
	first.RecordHit()
	first.RecordMiss()
	first.AddEvictions(2)
	first.RecordLazyExpiration()
	first.AddCleanupExpirations(3)
	first.ObserveRequest("GET", "/v1/cache/{key}", 200, time.Millisecond)
	if err := first.SetCacheStats(metrics.CacheStats{UsedBytes: 8, MaxBytes: 64, Items: 1}); err != nil {
		t.Fatal(err)
	}
	families := gather(t, second)
	for _, name := range []string{"cache_hits_total", "cache_misses_total", "cache_evictions_total"} {
		if got := counter(t, families, name, nil); got != 0 {
			t.Errorf("second instance %s = %g, want 0", name, got)
		}
	}
	for _, source := range []string{"lazy", "cleanup"} {
		if got := counter(t, families, "cache_expirations_total", map[string]string{"source": source}); got != 0 {
			t.Errorf("second instance %s expirations = %g, want 0", source, got)
		}
	}
	for _, name := range []string{"cache_used_bytes", "cache_max_bytes", "cache_items"} {
		if got := gauge(t, families, name); got != 0 {
			t.Errorf("second instance %s = %g, want 0", name, got)
		}
	}
	if families["cache_http_requests_total"] != nil || families["cache_http_request_duration_seconds"] != nil {
		t.Error("HTTP observations leaked between instances")
	}
	if got := counter(t, gather(t, first), "cache_hits_total", nil); got != 1 {
		t.Errorf("first instance hits = %g, want 1", got)
	}
}

func TestConcurrentUpdatesAndCollection(t *testing.T) {
	const workers, iterations = 16, 100
	m := metrics.New()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			<-start
			for i := range iterations {
				m.RecordHit()
				m.RecordMiss()
				m.AddEvictions(2)
				m.RecordLazyExpiration()
				m.AddCleanupExpirations(3)
				m.ObserveRequest("GET", "/v1/cache/{key}", 200, time.Millisecond)
				if err := m.SetCacheStats(metrics.CacheStats{UsedBytes: int64(i), MaxBytes: int64(2 * i), Items: i}); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	for range 2 {
		wg.Go(func() {
			<-start
			for range 20 {
				families := gather(t, m)
				items := gauge(t, families, "cache_items")
				if gauge(t, families, "cache_used_bytes") != items || gauge(t, families, "cache_max_bytes") != 2*items {
					t.Error("collection mixed cache gauge snapshots")
					return
				}
			}
		})
	}
	close(start)
	wg.Wait()
	families := gather(t, m)
	const total = workers * iterations
	for name, want := range map[string]float64{
		"cache_hits_total": total, "cache_misses_total": total, "cache_evictions_total": 2 * total,
	} {
		if got := counter(t, families, name, nil); got != want {
			t.Errorf("%s = %g, want %g", name, got, want)
		}
	}
	for source, want := range map[string]float64{"lazy": total, "cleanup": 3 * total} {
		if got := counter(t, families, "cache_expirations_total", map[string]string{"source": source}); got != want {
			t.Errorf("%s expirations = %g, want %g", source, got, want)
		}
	}
	if got := counter(t, families, "cache_http_requests_total",
		map[string]string{"method": "GET", "route": "/v1/cache/{key}", "status": "200"}); got != total {
		t.Errorf("requests = %g, want %d", got, total)
	}
	histogram := findMetric(t, families["cache_http_request_duration_seconds"],
		map[string]string{"method": "GET", "route": "/v1/cache/{key}"}).GetHistogram()
	if histogram.GetSampleCount() != total || math.Abs(histogram.GetSampleSum()-float64(total)*0.001) > 1e-9 {
		t.Errorf("lost concurrent duration observations: %v", histogram)
	}
}
