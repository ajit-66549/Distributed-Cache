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

	return mux
}
