// Package httpapi wires the HTTP router, middleware, and request handlers.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ErrorResponse is the standardized error envelope returned by the API.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody carries the machine-readable code, human message, and optional
// field-level details.
type ErrorBody struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string, details interface{}) {
	writeJSON(w, status, ErrorResponse{Error: ErrorBody{
		Code:    code,
		Message: message,
		Details: details,
	}})
}
