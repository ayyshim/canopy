// Package httpx holds JSON request/response helpers and the API error shape.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// Error is the body of every non-2xx API response:
// {"error": {"code": "...", "message": "...", "fields": {"email": "..."}}}
type Error struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

var (
	ErrNotFound     = NewError(http.StatusNotFound, "not_found", "Not found.")
	ErrUnauthorized = NewError(http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
	ErrForbidden    = NewError(http.StatusForbidden, "forbidden", "You don’t have permission to do that.")
	ErrBadJSON      = NewError(http.StatusBadRequest, "bad_request", "The request body isn’t valid JSON.")
)

// Validation collects per-field messages; Err returns nil when there are none.
type Validation map[string]string

func (v Validation) Add(field, msg string) {
	if _, ok := v[field]; !ok {
		v[field] = msg
	}
}

func (v Validation) Err() error {
	if len(v) == 0 {
		return nil
	}
	return &Error{Status: http.StatusUnprocessableEntity, Code: "validation", Message: "Check the highlighted fields.", Fields: v}
}

// FieldError is a single-field error with its own code, e.g. email_taken.
func FieldError(status int, code, field, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg, Fields: map[string]string{field: msg}}
}

func Decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return ErrBadJSON
	}
	return nil
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// Fail writes err as an API error; unknown errors become a logged 500.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		JSON(w, apiErr.Status, map[string]any{"error": apiErr})
		return
	}
	slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	JSON(w, http.StatusInternalServerError, map[string]any{"error": NewError(500, "internal", "Something went wrong. Try again.")})
}

// Handler adapts an error-returning handler to http.HandlerFunc.
func Handler(fn func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			Fail(w, r, err)
		}
	}
}
