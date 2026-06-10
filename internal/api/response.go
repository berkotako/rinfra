// Package api provides the chi-based HTTP handler layer. Handlers are thin:
// they decode requests, call services, encode responses. Business logic lives
// in internal/service.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/rinfra/rinfra/internal/domain"
	"github.com/rinfra/rinfra/internal/service"
	"github.com/rinfra/rinfra/internal/store"
)

// errorBody is the JSON error envelope sent on all non-2xx responses.
type errorBody struct {
	Error errDetail `json:"error"`
}

type errDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON encodes v as JSON and sets the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErrorCode writes the JSON error envelope with an explicit status and
// code, for validation errors that are not domain/store sentinels.
func writeErrorCode(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errDetail{Code: code, Message: message}})
}

// writeError maps a domain/store error to the correct HTTP status and error
// code, then writes the JSON error envelope.
func writeError(w http.ResponseWriter, log *slog.Logger, err error) {
	var status int
	var code string

	switch {
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
	case errors.Is(err, domain.ErrNotAuthorized):
		status = http.StatusForbidden
		code = "authorization_required"
	case errors.Is(err, domain.ErrAuthExpired):
		status = http.StatusForbidden
		code = "auth_expired"
	case errors.Is(err, domain.ErrOutsideWindow):
		status = http.StatusForbidden
		code = "outside_window"
	case errors.Is(err, domain.ErrEmptyScope):
		status = http.StatusForbidden
		code = "empty_scope"
	case errors.Is(err, service.ErrJobRunning):
		status = http.StatusConflict
		code = "job_running"
	default:
		status = http.StatusInternalServerError
		code = "internal_error"
		if log != nil {
			log.Error("unhandled API error", "err", err)
		}
	}

	writeJSON(w, status, errorBody{Error: errDetail{Code: code, Message: err.Error()}})
}

// decodeJSON decodes the request body into dst, writing a 400 on failure and
// returning false.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: errDetail{
			Code:    "bad_request",
			Message: "invalid JSON: " + err.Error(),
		}})
		return false
	}
	return true
}
