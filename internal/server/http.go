package server

import (
	"encoding/json"
	"net/http"
)

type Cache interface {
	Set(key string, value string)
	Get(key string) (string, bool)
	Delete(key string) bool
	Len() int
}

type Handler struct {
	cache Cache
}

func NewHandler(cache Cache) *Handler {
	return &Handler{
		cache: cache,
	}
}

type pingResponse struct {
	Status string `json:"status"`
}

func (h *Handler) Ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(pingResponse{
		Status: "ok",
	})
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ping", h.Ping)
	mux.HandleFunc("PUT /v1/cache/{key}", h.Set)
	mux.HandleFunc("GET /v1/cache/{key}", h.Get)
	mux.HandleFunc("DELETE /v1/cache/{key}", h.Delete)

	return mux
}

type setRequest struct {
	Value string `json:"value"`
}

func (h *Handler) Set(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	var request setRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	h.cache.Set(key, request.Value)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "stored",
	})
}

type getResponse struct {
	Value *string `json:"value"`
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	if key == "" {
		http.Error(w, `{"error":"key is required"}`, http.StatusBadRequest)
		return
	}

	value, found := h.cache.Get(key)

	w.Header().Set("Content-Type", "application/json")

	if !found {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(getResponse{
			Value: nil,
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(getResponse{
		Value: &value,
	})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	deleted := h.cache.Delete(key)

	w.Header().Set("Content-Type", "application/json")

	if !deleted {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "key not found",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
	})
}
