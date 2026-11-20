package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ajit-66549/distributed-cache/internal/cache"
	"github.com/ajit-66549/distributed-cache/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func monitoringRequest(handler http.Handler, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func TestMonitoringExportsSharedMetricsAndExcludesProbeTraffic(t *testing.T) {
	m := metrics.New()
	store, err := cache.NewStoreWithLimit(8, cache.WithMetrics(m))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("name", "Ajit"); err != nil {
		t.Fatal(err)
	}
	readiness := NewReadiness(context.Background(), func() bool { return true })
	readiness.MarkReady()
	handler := InstrumentHTTP(NewHandler(store, WithMonitoring(m.Gatherer(), readiness)).Routes(), m)
	if got := monitoringRequest(handler, "/v1/cache/name").Code; got != http.StatusOK {
		t.Fatalf("GET name status = %d", got)
	}

	for range 2 {
		for path, body := range map[string]string{
			"/healthz": "{\"status\":\"ok\"}\n",
			"/readyz":  "{\"status\":\"ready\"}\n",
		} {
			response := monitoringRequest(handler, path)
			if response.Code != http.StatusOK || response.Body.String() != body || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("%s: status=%d body=%q headers=%v", path, response.Code, response.Body.String(), response.Header())
			}
		}
		response := monitoringRequest(handler, "/metrics")
		if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain") {
			t.Fatalf("metrics response: status=%d headers=%v", response.Code, response.Header())
		}
		for _, sample := range []string{
			"cache_hits_total 1\n", "cache_used_bytes 8\n", "cache_max_bytes 8\n", "cache_items 1\n",
			`cache_http_requests_total{method="GET",route="/v1/cache/{key}",status="200"} 1`,
		} {
			if !strings.Contains(response.Body.String(), sample) {
				t.Errorf("metrics response missing %q", sample)
			}
		}
	}
	// Repeated probes and scrapes add neither HTTP observations nor engine GETs.
	assertHTTPMetrics(t, m, "GET", "/v1/cache/{key}", "200", 1)
}

func TestReadinessTransitionsAndLiveness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var writable atomic.Bool
	writable.Store(true)
	readiness := NewReadiness(ctx, writable.Load)
	handler := NewHandler(nil, WithMonitoring(metrics.New().Gatherer(), readiness)).Routes()
	assertStatus := func(want int) {
		t.Helper()
		response := monitoringRequest(handler, "/readyz")
		body := "{\"status\":\"not_ready\"}\n"
		if want == http.StatusOK {
			body = "{\"status\":\"ready\"}\n"
		}
		if response.Code != want || response.Body.String() != body {
			t.Fatalf("readiness status=%d body=%q, want %d %q", response.Code, response.Body.String(), want, body)
		}
		if response := monitoringRequest(handler, "/healthz"); response.Code != http.StatusOK {
			t.Fatalf("liveness changed to %d", response.Code)
		}
	}
	assertStatus(http.StatusServiceUnavailable) // Recovery/workers are not marked complete.
	readiness.MarkReady()
	assertStatus(http.StatusOK)
	writable.Store(false) // Published terminal persistence failure.
	assertStatus(http.StatusServiceUnavailable)
	readiness.MarkReady()
	assertStatus(http.StatusServiceUnavailable)

	// Shutdown is permanent even if a supplied persistence check becomes healthy.
	writable.Store(true)
	readiness.BeginShutdown()
	readiness.MarkReady()
	assertStatus(http.StatusServiceUnavailable)
	cancel()
	assertStatus(http.StatusServiceUnavailable)
}

func TestReadinessImmediatelyReflectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	readiness := NewReadiness(ctx, func() bool { return true })
	readiness.MarkReady()
	if !readiness.Ready() {
		t.Fatal("healthy initialized application is not ready")
	}
	cancel() // Signals withdraw readiness before the shutdown select loop runs.
	if readiness.Ready() {
		t.Fatal("canceled application remained ready")
	}
}

type failingMetricsGatherer struct{}

func (failingMetricsGatherer) Gather() ([]*dto.MetricFamily, error) {
	return nil, errors.New("collection failed")
}

func TestMonitoringCollectionFailureAndMethodHandling(t *testing.T) {
	for _, gatherer := range []prometheus.Gatherer{nil, failingMetricsGatherer{}} {
		handler := NewHandler(nil, WithMonitoring(gatherer, nil)).Routes()
		if response := monitoringRequest(handler, "/metrics"); response.Code != http.StatusInternalServerError {
			t.Errorf("failed collection status = %d, want 500", response.Code)
		}
		if response := monitoringRequest(handler, "/readyz"); response.Code != http.StatusServiceUnavailable {
			t.Errorf("unconfigured readiness status = %d, want 503", response.Code)
		}
		for _, path := range []string{"/metrics", "/healthz", "/readyz"} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
			if response.Code != http.StatusMethodNotAllowed {
				t.Errorf("POST %s status = %d, want 405", path, response.Code)
			}
		}
	}
}
