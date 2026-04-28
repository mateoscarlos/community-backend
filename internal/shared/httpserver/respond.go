package httpserver

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse is the canonical JSON error body for all API errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

// WriteJSON writes any value as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, ErrorResponse{Error: message})
}
