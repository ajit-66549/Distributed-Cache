package server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/cache"
	"github.com/ajit-66549/distributed-cache/internal/metrics"
	dto "github.com/prometheus/client_model/go"
)

// Each scenario expects a single label combination for each HTTP metric family.
func assertHTTPMetrics(t *testing.T, m *metrics.Metrics, method, route, status string, count uint64) *dto.Histogram {
	t.Helper()
	families, err := m.Gatherer().Gather()
	if err != nil {
		t.Fatal(err)
	}
	var histogram *dto.Histogram
	foundCounter := false
	for _, family := range families {
		if family.GetName() != "cache_http_requests_total" && family.GetName() != "cache_http_request_duration_seconds" {
			continue
		}
		if len(family.GetMetric()) != 1 {
			t.Fatalf("%s has %d series, want 1", family.GetName(), len(family.GetMetric()))
		}
		sample := family.GetMetric()[0]
		wantLabels := map[string]string{"method": method, "route": route}
		if family.GetName() == "cache_http_requests_total" {
			wantLabels["status"] = status
			foundCounter = true
			if sample.Counter == nil || sample.GetCounter().GetValue() != float64(count) {
				t.Errorf("request counter = %v, want %d", sample.Counter, count)
			}
		} else {
			histogram = sample.Histogram
			if histogram == nil || histogram.GetSampleCount() != count {
				t.Errorf("duration histogram = %v, want %d observations", histogram, count)
			}
		}
		if len(sample.GetLabel()) != len(wantLabels) {
			t.Errorf("%s has unexpected labels: %v", family.GetName(), sample.GetLabel())
		}
		for _, label := range sample.GetLabel() {
			if want, ok := wantLabels[label.GetName()]; !ok || label.GetValue() != want {
				t.Errorf("unexpected label %s=%q", label.GetName(), label.GetValue())
			}
		}
	}
	if !foundCounter || histogram == nil {
		t.Fatal("request counter or duration histogram was not collected")
	}
	return histogram
}

func TestHTTPMetricsStatusesAndRoutes(t *testing.T) {
	for _, test := range []struct {
		name, method, path, body, route string
		methodLabel                     string
		status                          int
		writeErr                        error
	}{
		{name: "hit", method: "GET", path: "/v1/cache/name", route: "/v1/cache/{key}", status: 200},
		{name: "miss", method: "GET", path: "/v1/cache/missing", route: "/v1/cache/{key}", status: 404},
		{name: "stored", method: "PUT", path: "/v1/cache/name", body: `{"value":"Ajit"}`, route: "/v1/cache/{key}", status: 201},
		{name: "deleted", method: "DELETE", path: "/v1/cache/name", route: "/v1/cache/{key}", status: 200},
		{name: "invalid JSON", method: "PUT", path: "/v1/cache/name", body: `{`, route: "/v1/cache/{key}", status: 400},
		{name: "capacity rejection", method: "PUT", path: "/v1/cache/name", body: `{"value":"Ajit"}`, route: "/v1/cache/{key}", status: 413, writeErr: cache.ErrEntryTooLarge},
		{name: "server failure", method: "PUT", path: "/v1/cache/name", body: `{"value":"Ajit"}`, route: "/v1/cache/{key}", status: 500, writeErr: errors.New("persistence failed")},
		{name: "unsupported method", method: "POST", methodLabel: "OTHER", path: "/v1/cache/name", route: "unmatched", status: 405},
		{name: "unknown path", method: "GET", path: "/unknown", route: "unmatched", status: 404},
		{name: "ping", method: "GET", path: "/v1/ping", route: "/v1/ping", status: 200},
		{name: "HEAD", method: "HEAD", path: "/v1/cache/name", route: "/v1/cache/{key}", status: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := metrics.New()
			store := &fakeCache{key: "name", value: "Ajit", found: true, writeErr: test.writeErr}
			handler := InstrumentHTTP(NewHandler(store).Routes(), m)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
			if response.Code != test.status {
				t.Fatalf("HTTP status = %d, want %d", response.Code, test.status)
			}
			methodLabel := test.method
			if test.methodLabel != "" {
				methodLabel = test.methodLabel
			}
			histogram := assertHTTPMetrics(t, m, methodLabel, test.route, strconv.Itoa(test.status), 1)
			if histogram.GetSampleSum() <= 0 {
				t.Error("request duration was not recorded")
			}
		})
	}
}

func TestHTTPMetricsGroupsKeysByPattern(t *testing.T) {
	m := metrics.New()
	handler := InstrumentHTTP(NewHandler(&fakeCache{}).Routes(), m)
	for _, path := range []string{"/v1/cache/name", "/v1/cache/city", "/v1/cache/session?token=secret"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	assertHTTPMetrics(t, m, "GET", "/v1/cache/{key}", "404", 3)
}

// ResponseRecorder does not model informational responses, so omit them from its
// final-status tracking while still forwarding them through our middleware.
type informationalResponseWriter struct{ *httptest.ResponseRecorder }

func (w *informationalResponseWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		return
	}
	w.ResponseRecorder.WriteHeader(status)
}

func TestHTTPMetricsFinalStatusAndPanics(t *testing.T) {
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
		status  string
		panics  bool
	}{
		{name: "empty response", status: "200", handler: func(http.ResponseWriter, *http.Request) {}},
		{name: "implicit response", status: "200", handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }},
		{name: "first final status", status: "201", handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201); w.WriteHeader(500) }},
		{name: "informational then final", status: "202", handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(103); w.WriteHeader(202) }},
		{name: "panic before headers", status: "aborted", panics: true, handler: func(http.ResponseWriter, *http.Request) { panic("handler panic") }},
		{name: "panic after headers", status: "500", panics: true, handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500); panic("handler panic") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := metrics.New()
			mux := http.NewServeMux()
			mux.HandleFunc("GET /v1/cache/{key}", test.handler)
			handler := InstrumentHTTP(mux, m)
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				handler.ServeHTTP(&informationalResponseWriter{httptest.NewRecorder()}, httptest.NewRequest("GET", "/v1/cache/name", nil))
			}()
			if test.panics && recovered != "handler panic" {
				t.Fatalf("handler panic was changed or swallowed: %v", recovered)
			}
			if !test.panics && recovered != nil {
				t.Fatalf("unexpected panic: %v", recovered)
			}
			assertHTTPMetrics(t, m, "GET", "/v1/cache/{key}", test.status, 1)
		})
	}
}

func TestHTTPMetricsExcludesProbesAndProfiling(t *testing.T) {
	m := metrics.New()
	called := 0
	handler := InstrumentHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}), m)
	paths := []string{"/metrics", "/healthz", "/readyz", "/debug/pprof", "/debug/pprof/", "/debug/pprof/profile"}
	for _, path := range paths {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if called != len(paths) {
		t.Fatalf("excluded requests reached the handler %d times, want %d", called, len(paths))
	}
	families, err := m.Gatherer().Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if strings.HasPrefix(family.GetName(), "cache_http_") {
			t.Errorf("excluded paths created %s", family.GetName())
		}
	}
}

type delayedRequestBody struct{ io.Reader }

func (r delayedRequestBody) Read(body []byte) (int, error) {
	time.Sleep(5 * time.Millisecond)
	return r.Reader.Read(body)
}

type delayedSetCache struct{ Cache }

func (c delayedSetCache) Set(key, value string) error {
	time.Sleep(5 * time.Millisecond)
	return c.Cache.Set(key, value)
}

type delayedBodyWriter struct{ http.ResponseWriter }

func (w delayedBodyWriter) Write(body []byte) (int, error) {
	time.Sleep(5 * time.Millisecond)
	return w.ResponseWriter.Write(body)
}

func TestHTTPMetricsTimesValidationCacheAndResponse(t *testing.T) {
	m := metrics.New()
	handler := InstrumentHTTP(NewHandler(delayedSetCache{&fakeCache{}}).Routes(), m)
	body := delayedRequestBody{strings.NewReader(`{"value":"Ajit"}`)}
	response := httptest.NewRecorder()
	handler.ServeHTTP(delayedBodyWriter{response}, httptest.NewRequest("PUT", "/v1/cache/name", body))
	if response.Code != http.StatusCreated {
		t.Fatalf("HTTP status = %d, want 201", response.Code)
	}
	histogram := assertHTTPMetrics(t, m, "PUT", "/v1/cache/{key}", "201", 1)
	if histogram.GetSampleSum() < (15 * time.Millisecond).Seconds() {
		t.Errorf("duration %g seconds excluded request decoding, cache work, or response writing", histogram.GetSampleSum())
	}
}
