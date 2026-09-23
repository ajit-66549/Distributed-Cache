package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// Test for Set

type fakeCache struct {
	key   string
	value string
}

func (f *fakeCache) Set(key, value string) {
	f.key = key
	f.value = value
}

func (f *fakeCache) Get(key string) (string, bool) {
	return "", false
}

func (f *fakeCache) Delete(key string) bool {
	return false
}

func (f *fakeCache) Len() int {
	return 0
}

func TestSet(t *testing.T) {
	cache := &fakeCache{}
	handler := NewHandler(cache)

	body := strings.NewReader(`{"value":"Ajit"}`)
	request := httptest.NewRequest(
		http.MethodPut,
		"/v1/cache/name",
		body,
	)
	recorder := httptest.NewRecorder()

	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Errorf(
			"expected status %d, got %d",
			http.StatusCreated,
			recorder.Code,
		)
	}

	if cache.key != "name" {
		t.Errorf("expected key %q, got %q", "name", cache.key)
	}

	if cache.value != "Ajit" {
		t.Errorf("expected value %q, got %q", "Ajit", cache.value)
	}

	expectedBody := `{"status":"stored"}` + "\n"

	if recorder.Body.String() != expectedBody {
		t.Errorf(
			"expected body %q, got %q",
			expectedBody,
			recorder.Body.String(),
		)
	}
}

func TestSetRejectsInvalidJSON(t *testing.T) {
	cache := &fakeCache{}
	handler := NewHandler(cache)

	body := strings.NewReader(`{"value":`)
	request := httptest.NewRequest(
		http.MethodPut,
		"/v1/cache/name",
		body,
	)
	recorder := httptest.NewRecorder()

	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf(
			"expected status %d, got %d",
			http.StatusBadRequest,
			recorder.Code,
		)
	}

	if cache.key != "" || cache.value != "" {
		t.Fatal("cache should not be updated for invalid JSON")
	}
}

// Test for Get

type fakeGetCache struct {
	key   string
	value string
	found bool
}

func (f *fakeGetCache) Set(key, value string) {
	f.key = key
	f.value = value
	f.found = true
}

func (f *fakeGetCache) Get(key string) (string, bool) {
	if !f.found || key != f.key {
		return "", false
	}

	return f.value, true
}

func (f *fakeGetCache) Delete(key string) bool {
	return false
}

func (f *fakeGetCache) Len() int {
	if f.found {
		return 1
	}

	return 0
}

func TestGetExistingKey(t *testing.T) {
	cache := &fakeGetCache{}
	cache.Set("name", "Ajit")

	handler := NewHandler(cache)

	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/cache/name",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Errorf(
			"expected status %d, got %d",
			http.StatusOK,
			recorder.Code,
		)
	}

	expectedBody := `{"value":"Ajit"}` + "\n"

	if recorder.Body.String() != expectedBody {
		t.Errorf(
			"expected body %q, got %q",
			expectedBody,
			recorder.Body.String(),
		)
	}

	contentType := recorder.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf(
			"expected Content-Type application/json, got %q",
			contentType,
		)
	}
}

func TestGetMissingKey(t *testing.T) {
	cache := &fakeGetCache{}
	handler := NewHandler(cache)

	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/cache/missing",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Errorf(
			"expected status %d, got %d",
			http.StatusNotFound,
			recorder.Code,
		)
	}

	expectedBody := `{"value":null}` + "\n"

	if recorder.Body.String() != expectedBody {
		t.Errorf(
			"expected body %q, got %q",
			expectedBody,
			recorder.Body.String(),
		)
	}
}
