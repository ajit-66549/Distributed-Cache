package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Option configures an HTTP handler before its routes are constructed.
type Option func(*Handler)

// WithMonitoring exposes health, readiness, and the supplied metrics registry.
// These endpoints do not change the behavior of the cache API.
func WithMonitoring(gatherer prometheus.Gatherer, readiness *Readiness) Option {
	return func(handler *Handler) {
		var exporter http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "metrics unavailable", http.StatusInternalServerError)
		})
		if gatherer != nil {
			exporter = promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
				ErrorHandling: promhttp.HTTPErrorOnError,
			})
		}
		handler.monitoring = &monitoringEndpoints{exporter: exporter, readiness: readiness}
	}
}

type monitoringEndpoints struct {
	exporter  http.Handler
	readiness *Readiness
}

func (m *monitoringEndpoints) register(mux *http.ServeMux) {
	mux.Handle("GET /metrics", m.exporter)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !m.readiness.Ready() {
			writeJSON(w, http.StatusServiceUnavailable, statusResponse{Status: "not_ready"})
			return
		}
		writeJSON(w, http.StatusOK, statusResponse{Status: "ready"})
	})
}
