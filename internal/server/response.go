package server

import (
	"encoding/json"
	"net/http"
)

type statusResponse struct {
	Status string `json:"status"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(data)
}
