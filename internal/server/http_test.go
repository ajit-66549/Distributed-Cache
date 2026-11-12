package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/cache"
)

type fakeCache struct {
	key              string
	value            string
	found            bool
	setCalled        bool
	setWithTTLCalled bool
	ttl              time.Duration
	writeErr         error
}

func (f *fakeCache) Set(key, value string) error {
	f.setCalled = true
	if f.writeErr != nil {
		return f.writeErr
	}
	f.key = key
	f.value = value
	f.found = true
	return nil
}

func (f *fakeCache) SetWithTTL(
	key string,
	value string,
	ttl time.Duration,
) error {
	f.setWithTTLCalled = true
	if f.writeErr != nil {
		return f.writeErr
	}
	f.key = key
	f.value = value
	f.found = true
	f.ttl = ttl
	return nil
}

func (f *fakeCache) Get(key string) (string, bool) {
	if !f.found || key != f.key {
		return "", false
	}

	return f.value, true
}

func (f *fakeCache) Delete(key string) (bool, error) {
	if !f.found || key != f.key {
		return false, nil
	}

	f.key = ""
	f.value = ""
	f.found = false

	return true, nil
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

func TestSetReturnsWriteErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "oversized", err: cache.ErrEntryTooLarge, status: http.StatusRequestEntityTooLarge},
		{name: "wrapped oversized", err: fmt.Errorf("write failed: %w", cache.ErrEntryTooLarge), status: http.StatusRequestEntityTooLarge},
		{name: "unexpected", err: errors.New("storage unavailable"), status: http.StatusInternalServerError},
	}
	for _, withTTL := range []bool{false, true} {
		for _, test := range tests {
			t.Run(fmt.Sprintf("TTL=%t/%s", withTTL, test.name), func(t *testing.T) {
				fake := &fakeCache{writeErr: test.err}
				body := `{"value":"Ajit"}`
				if withTTL {
					body = `{"value":"Ajit","ttl_seconds":60}`
				}
				request := httptest.NewRequest(http.MethodPut, "/v1/cache/name", strings.NewReader(body))
				recorder := httptest.NewRecorder()
				NewHandler(fake).Routes().ServeHTTP(recorder, request)

				if recorder.Code != test.status {
					t.Errorf("status is %d, want %d", recorder.Code, test.status)
				}
				var response errorResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatalf("expected one JSON error response, got %q: %v", recorder.Body.String(), err)
				}
				if response.Error != test.err.Error() {
					t.Errorf("error is %q, want %q", response.Error, test.err.Error())
				}
				if got := recorder.Header().Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type is %q, want application/json", got)
				}
				if fake.setWithTTLCalled != withTTL || fake.setCalled == withTTL {
					t.Error("called the wrong cache write method")
				}
				if fake.found {
					t.Error("failed write mutated the cache")
				}
			})
		}
	}
}

func TestHTTPMemoryBudget(t *testing.T) {
	for _, withTTL := range []bool{false, true} {
		t.Run(fmt.Sprintf("TTL=%t", withTTL), func(t *testing.T) {
			store, err := cache.NewStoreWithLimit(8)
			if err != nil {
				t.Fatal(err)
			}
			routes := NewHandler(store).Routes()
			setBody := func(value string) string {
				if withTTL {
					return fmt.Sprintf(`{"value":%q,"ttl_seconds":60}`, value)
				}
				return fmt.Sprintf(`{"value":%q}`, value)
			}
			steps := []struct {
				name       string
				method     string
				key        string
				body       string
				wantStatus int
				wantBody   string
			}{
				{"insert a", http.MethodPut, "a", setBody("xxx"), http.StatusCreated, `{"status":"stored"}`},
				{"insert b", http.MethodPut, "b", setBody("xxx"), http.StatusCreated, `{"status":"stored"}`},
				{"touch a", http.MethodGet, "a", "", http.StatusOK, `{"value":"xxx"}`},
				{"insert c evicts b", http.MethodPut, "c", setBody("xxx"), http.StatusCreated, `{"status":"stored"}`},
				{"b is missing", http.MethodGet, "b", "", http.StatusNotFound, `{"value":null}`},
				{"oversized update", http.MethodPut, "a", setBody("xxxxxxxx"), http.StatusRequestEntityTooLarge, `{"error":"entry exceeds cache memory budget"}`},
				{"oversized insert", http.MethodPut, "d", setBody("xxxxxxxx"), http.StatusRequestEntityTooLarge, `{"error":"entry exceeds cache memory budget"}`},
				{"a is preserved", http.MethodGet, "a", "", http.StatusOK, `{"value":"xxx"}`},
				{"c is preserved", http.MethodGet, "c", "", http.StatusOK, `{"value":"xxx"}`},
				{"delete a", http.MethodDelete, "a", "", http.StatusOK, `{"status":"deleted"}`},
				{"exact fit", http.MethodPut, "a", setBody("xxxxxxx"), http.StatusCreated, `{"status":"stored"}`},
				{"c was evicted", http.MethodGet, "c", "", http.StatusNotFound, `{"value":null}`},
				{"exact fit is readable", http.MethodGet, "a", "", http.StatusOK, `{"value":"xxxxxxx"}`},
			}
			for _, step := range steps {
				request := httptest.NewRequest(step.method, "/v1/cache/"+step.key, strings.NewReader(step.body))
				recorder := httptest.NewRecorder()
				routes.ServeHTTP(recorder, request)
				if recorder.Code != step.wantStatus || recorder.Body.String() != step.wantBody+"\n" {
					t.Fatalf("%s: got (%d, %q), want (%d, %q)", step.name, recorder.Code, recorder.Body.String(), step.wantStatus, step.wantBody+"\n")
				}
			}
		})
	}
}

func TestDeleteRejectsInvalidKey(t *testing.T) {
	key := strings.Repeat("k", maxKeyLength+1)
	fake := &fakeCache{key: key, value: "keep", found: true}
	request := httptest.NewRequest(http.MethodDelete, "/v1/cache/"+key, nil)
	recorder := httptest.NewRecorder()
	NewHandler(fake).Routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status is %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var response errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Errorf("expected one JSON error response, got %q: %v", recorder.Body.String(), err)
	}
	if !fake.found || fake.value != "keep" {
		t.Error("invalid DELETE request changed the cache")
	}
}
