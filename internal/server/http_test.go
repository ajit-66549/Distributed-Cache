package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeCache struct {
	key              string
	value            string
	found            bool
	setCalled        bool
	setWithTTLCalled bool
	ttl              time.Duration
}

func (f *fakeCache) Set(key, value string) {
	f.key = key
	f.value = value
	f.found = true
	f.setCalled = true
}

func (f *fakeCache) SetWithTTL(
	key string,
	value string,
	ttl time.Duration,
) {
	f.key = key
	f.value = value
	f.found = true
	f.setWithTTLCalled = true
	f.ttl = ttl
}

func (f *fakeCache) Get(key string) (string, bool) {
	if !f.found || key != f.key {
		return "", false
	}

	return f.value, true
}

func (f *fakeCache) Delete(key string) bool {
	if !f.found || key != f.key {
		return false
	}

	f.key = ""
	f.value = ""
	f.found = false

	return true
}

func (f *fakeCache) Len() int {
	if f.found {
		return 1
	}

	return 0
}

func TestPing(t *testing.T) {
	handler := NewHandler(nil)

	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/ping",
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

	expectedBody := "{\"status\":\"ok\"}\n"

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

	if !cache.setCalled {
		t.Fatal("expected Set to be called")
	}

	if cache.setWithTTLCalled {
		t.Error("expected SetWithTTL not to be called")
	}

	if !cache.found ||
		cache.key != "name" ||
		cache.value != "Ajit" {
		t.Errorf(
			"expected name=Ajit in cache, got %+v",
			cache,
		)
	}

	expectedBody := "{\"status\":\"stored\"}\n"

	if recorder.Body.String() != expectedBody {
		t.Errorf(
			"expected body %q, got %q",
			expectedBody,
			recorder.Body.String(),
		)
	}
}

func TestSetWithTTL(t *testing.T) {
	cache := &fakeCache{}
	handler := NewHandler(cache)

	body := strings.NewReader(
		`{"value":"Ajit","ttl_seconds":60}`,
	)

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

	if !cache.setWithTTLCalled {
		t.Fatal("expected SetWithTTL to be called")
	}

	if cache.setCalled {
		t.Error("expected Set not to be called")
	}

	if cache.key != "name" {
		t.Errorf(
			"expected key %q, got %q",
			"name",
			cache.key,
		)
	}

	if cache.value != "Ajit" {
		t.Errorf(
			"expected value %q, got %q",
			"Ajit",
			cache.value,
		)
	}

	if cache.ttl != 60*time.Second {
		t.Errorf(
			"expected TTL %v, got %v",
			60*time.Second,
			cache.ttl,
		)
	}

	expectedBody := "{\"status\":\"stored\"}\n"

	if recorder.Body.String() != expectedBody {
		t.Errorf(
			"expected body %q, got %q",
			expectedBody,
			recorder.Body.String(),
		)
	}
}

func TestSetRejectsInvalidTTL(t *testing.T) {
	cache := &fakeCache{}
	handler := NewHandler(cache)

	body := strings.NewReader(
		`{"value":"Ajit","ttl_seconds":0}`,
	)

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

	if cache.setCalled {
		t.Error("expected Set not to be called")
	}

	if cache.setWithTTLCalled {
		t.Error("expected SetWithTTL not to be called")
	}

	if cache.found {
		t.Error("cache should not be updated for invalid TTL")
	}
}

func TestSetRejectsNegativeTTL(t *testing.T) {
	cache := &fakeCache{}
	handler := NewHandler(cache)

	body := strings.NewReader(
		`{"value":"Ajit","ttl_seconds":-10}`,
	)

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

	if cache.setCalled || cache.setWithTTLCalled {
		t.Error("expected no cache write for negative TTL")
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

	if cache.found {
		t.Fatal("cache should not be updated for invalid JSON")
	}
}

func TestGetExistingKey(t *testing.T) {
	cache := &fakeCache{}
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

	expectedBody := "{\"value\":\"Ajit\"}\n"

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
	handler := NewHandler(&fakeCache{})

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

	expectedBody := "{\"value\":null}\n"

	if recorder.Body.String() != expectedBody {
		t.Errorf(
			"expected body %q, got %q",
			expectedBody,
			recorder.Body.String(),
		)
	}
}

func TestDeleteExistingKey(t *testing.T) {
	cache := &fakeCache{}
	cache.Set("name", "Ajit")

	handler := NewHandler(cache)

	request := httptest.NewRequest(
		http.MethodDelete,
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

	expectedBody := "{\"status\":\"deleted\"}\n"

	if recorder.Body.String() != expectedBody {
		t.Errorf(
			"expected body %q, got %q",
			expectedBody,
			recorder.Body.String(),
		)
	}

	if _, found := cache.Get("name"); found {
		t.Fatal("expected key to be removed from cache")
	}
}

func TestDeleteMissingKey(t *testing.T) {
	handler := NewHandler(&fakeCache{})

	request := httptest.NewRequest(
		http.MethodDelete,
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

	expectedBody := "{\"error\":\"key not found\"}\n"

	if recorder.Body.String() != expectedBody {
		t.Errorf(
			"expected body %q, got %q",
			expectedBody,
			recorder.Body.String(),
		)
	}
}
