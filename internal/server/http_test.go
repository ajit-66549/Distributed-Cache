package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPing(t *testing.T) {
	handler := NewHandler(nil)

	request := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
	recorder := httptest.NewRecorder()

	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	expectedBody := `{"status":"ok"}` + "\n"

	if recorder.Body.String() != expectedBody {
		t.Errorf(
			"expected body %q, got %q",
			expectedBody,
			recorder.Body.String(),
		)
	}

	contentType := recorder.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}
}
