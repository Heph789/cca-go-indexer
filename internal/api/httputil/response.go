// Package httputil provides shared HTTP response helpers used by
// both the api and handlers packages. Extracted to avoid circular
// imports between api (server/middleware) and api/handlers.
package httputil

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// --- JSON response envelope types ---

// Response is the standard success envelope.
// All API responses wrap their payload in {"data": ...} for consistency
// and forward-compatibility (easy to add top-level "meta" later).
type Response struct {
	Data any `json:"data"`
}

// ErrorBody is the standard error envelope.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains the machine-readable code and human-readable message.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// --- Response helper functions ---

// WriteJSON serializes v as JSON and writes it with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// WriteError writes a structured error response.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorBody{
		Error: ErrorDetail{Code: code, Message: message},
	})
}

// WriteNotFound writes a 404 error with a consistent message format.
func WriteNotFound(w http.ResponseWriter, resource string) {
	WriteError(w, http.StatusNotFound, "not_found", resource+" not found")
}
