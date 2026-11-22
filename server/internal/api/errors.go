package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"anki-app/server/internal/store"
	"anki-app/server/internal/validate"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError writes {"error":{"code","message"}}.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: msg}})
}

// mapErr maps an error to a status, code and client-safe message. This is the
// only place errors become API responses.
func mapErr(err error) (status int, code, msg string) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return http.StatusRequestEntityTooLarge, "too_large", "request body is larger than 1 MB"
	case errors.Is(err, validate.ErrInvalid):
		return http.StatusBadRequest, "invalid_input", validationMessage(err)
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "not_found", "not found"
	case errors.Is(err, store.ErrConflict):
		return http.StatusConflict, "conflict", "a deck with that name already exists"
	case errors.Is(err, store.ErrStale):
		return http.StatusConflict, "stale", "the card has changed; reload it"
	}
	return http.StatusInternalServerError, "internal", "internal error"
}

// validationMessage returns the text after the last "invalid: " marker; it
// names the field and the rule, never internal details.
func validationMessage(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, "invalid: "); i >= 0 {
		return s[i+len("invalid: "):]
	}
	return "invalid input"
}

// fail writes the mapped error; 500s are logged (method, path, error) and
// never include card content.
func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg := mapErr(err)
	if status == http.StatusInternalServerError {
		s.Logger.Error("request failed", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.Any("error", err))
	}
	writeError(w, status, code, msg)
}
