package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/cache"
	"github.com/ajit-66549/distributed-cache/internal/persistence"
)

type Cache interface {
	Set(key string, value string) error
	SetWithTTL(key string, value string, ttl time.Duration) error
	Get(key string) (string, bool)
	Delete(key string) (bool, error)
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
	Value      string `json:"value"`
	TTLSeconds *int64 `json:"ttl_seconds,omitempty"`
}

func (h *Handler) Set(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	var request setRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "invalid JSON",
		})
		return
	}

	if err := validateKey(key); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: err.Error(),
		})
		return
	}

	if err := valueValidate(request.Value); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: err.Error(),
		})
		return
	}

	if err := validateTTL(request.TTLSeconds); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: err.Error(),
		})
		return
	}

	var err error
	if request.TTLSeconds != nil {
		ttl := time.Duration(*request.TTLSeconds) * time.Second
		err = h.cache.SetWithTTL(key, request.Value, ttl)
	} else {
		err = h.cache.Set(key, request.Value)
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, cache.ErrEntryTooLarge) || errors.Is(err, persistence.ErrRecordTooLarge) {
			status = http.StatusRequestEntityTooLarge
		} else if errors.Is(err, cache.ErrExpirationOutOfRange) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, errorResponse{
			Error: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusCreated, statusResponse{
		Status: "stored",
	})
}

type getResponse struct {
	Value *string `json:"value"`
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	if err := validateKey(key); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: err.Error(),
		})
		return
	}

	value, found := h.cache.Get(key)

	if !found {
		writeJSON(w, http.StatusNotFound, getResponse{
			Value: nil,
		})
		return
	}

	writeJSON(w, http.StatusOK, getResponse{
		Value: &value,
	})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	if err := validateKey(key); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: err.Error(),
		})
		return
	}

	deleted, err := h.cache.Delete(key)
	if err != nil {
		log.Printf("cache delete failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "failed to delete key",
		})
		return
	}

	if !deleted {
		writeJSON(w, http.StatusNotFound, errorResponse{
			Error: "key not found",
		})
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{
		Status: "deleted",
	})
}
