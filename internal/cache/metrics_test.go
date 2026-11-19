package cache

import (
	"testing"

	"github.com/ajit-66549/distributed-cache/internal/metrics"
)

func TestGetRecordsHitsAndMisses(t *testing.T) {
	measurements := metrics.New()
	store := NewStore(WithMetrics(measurements))
	if err := store.Set("name", "Ajit"); err != nil {
		t.Fatalf("store name: %v", err)
	}

	// Two successful reads should record two hits.
	for i := 0; i < 2; i++ {
		if value, found := store.Get("name"); !found || value != "Ajit" {
			t.Fatalf("read %d: got (%q, %v), want (Ajit, true)", i+1, value, found)
		}
	}
	// One absent-key read should record one miss.
	if value, found := store.Get("missing"); found || value != "" {
		t.Fatalf("missing read: got (%q, %v), want (empty, false)", value, found)
	}

	// Inspect the exported counters instead of calling recording methods directly.
	families, err := measurements.Gatherer().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	want := map[string]float64{
		"cache_hits_total":   2,
		"cache_misses_total": 1,
	}
	for _, family := range families {
		expected, ok := want[family.GetName()]
		if !ok {
			continue
		}
		samples := family.GetMetric()
		if len(samples) != 1 || samples[0].Counter == nil {
			t.Fatalf("%s: expected one counter sample", family.GetName())
		}
		got := samples[0].GetCounter().GetValue()
		if got != expected {
			t.Errorf("%s = %g, want %g", family.GetName(), got, expected)
		}
		t.Logf("%s = %g", family.GetName(), got)
		delete(want, family.GetName())
	}
	for name := range want {
		t.Errorf("%s was not collected", name)
	}
}
