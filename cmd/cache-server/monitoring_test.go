//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ajit-66549/distributed-cache/internal/cache"
	"github.com/ajit-66549/distributed-cache/internal/metrics"
	"github.com/ajit-66549/distributed-cache/internal/persistence"
	"github.com/ajit-66549/distributed-cache/internal/server"
)

func TestMonitoringReadinessTracksRecoveredPersistence(t *testing.T) {
	cfg := startupTestConfig(t.TempDir(), 32)
	store, writer, err := openStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if err := store.Set("name", "Ajit"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("city", "Pune"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	m := metrics.New()
	restored, reopened, err := openStore(cfg, cache.WithMetrics(m))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if restored.Len() != 2 {
		t.Fatal("snapshot and newer AOF entries were not both recovered")
	}
	readiness := server.NewReadiness(context.Background(), reopened.Writable)
	handler := server.InstrumentHTTP(server.NewHandler(restored, server.WithMonitoring(m.Gatherer(), readiness)).Routes(), m)
	request := func(method, path, body string, want int) {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		if response.Code != want {
			t.Fatalf("%s %s status=%d body=%q, want %d", method, path, response.Code, response.Body.String(), want)
		}
	}
	request("GET", "/readyz", "", 503) // Worker/application initialization is not complete yet.
	readiness.MarkReady()
	request("GET", "/readyz", "", 200)
	request("PUT", "/v1/cache/large", `{"value":"`+strings.Repeat("x", 32)+`"}`, 413)
	request("GET", "/v1/cache/missing", "", 404)
	request("GET", "/readyz", "", 200) // Rejections and normal misses keep readiness.

	// Force a real rotation failure after snapshot publication in this temporary directory.
	path := filepath.Join(cfg.DataDir, "append.aof")
	if err := os.Rename(path, filepath.Join(cfg.DataDir, "retained.aof")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := restored.SaveSnapshot(); !errors.Is(err, persistence.ErrAOFFailed) {
		t.Fatalf("snapshot rotation error=%v, want terminal persistence failure", err)
	}
	request("GET", "/readyz", "", 503)
	request("GET", "/v1/cache/name", "", 200) // Reads can continue but cannot repair persistence.
	readiness.MarkReady()
	request("GET", "/readyz", "", 503)
	request("GET", "/healthz", "", 200)
	request("GET", "/metrics", "", 200)
}
