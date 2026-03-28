package httputil

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

const (
	CodeBadRequest    = "bad_request"
	CodeNotFound      = "not_found"
	CodeInternalError = "internal_error"
)

type Response struct {
	Data any `json:"data"`
}

type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorBody{
		Error: ErrorDetail{Code: code, Message: message},
	})
}

func WriteNotFound(w http.ResponseWriter, resource string) {
	WriteError(w, http.StatusNotFound, CodeNotFound, resource+" not found")
}
