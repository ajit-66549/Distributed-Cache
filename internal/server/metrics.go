package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/metrics"
)

// InstrumentHTTP records application requests around the entire router, including
// validation, cache lock waits, persistence, and response encoding. Passing nil
// measurements leaves the handler unchanged. Install it once per HTTP server.
func InstrumentHTTP(next http.Handler, measurements *metrics.Metrics) http.Handler {
	if measurements == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check the path before routing: these endpoints may not be registered yet.
		switch r.URL.Path {
		case "/metrics", "/healthz", "/readyz", "/debug/pprof":
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/debug/pprof/") {
			next.ServeHTTP(w, r)
			return
		}

		started := time.Now()
		response := &metricsResponseWriter{ResponseWriter: w}
		completed := false
		defer func() {
			elapsed := time.Since(started)
			status := response.status
			if status == 0 && completed {
				status = http.StatusOK // An empty response also defaults to 200.
			}
			// ServeMux sets Pattern during routing, including before a handler panic.
			// Strip its method prefix; never use the request's key or query as a label.
			route := r.Pattern
			if _, path, ok := strings.Cut(route, " "); ok {
				route = path
			}
			measurements.ObserveRequest(r.Method, route, status, elapsed)
		}()

		next.ServeHTTP(response, r)
		completed = true
	})
}

type metricsResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *metricsResponseWriter) WriteHeader(status int) {
	w.ResponseWriter.WriteHeader(status)
	// Informational headers do not replace the final response status. Later
	// WriteHeader calls likewise must not replace an already-recorded final one.
	if w.status == 0 && status >= 200 {
		w.status = status
	}
}

func (w *metricsResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

// Unwrap lets http.ResponseController reach the underlying response writer.
func (w *metricsResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
