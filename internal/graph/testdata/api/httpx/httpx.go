// Package httpx holds response helpers like the ones real projects write:
// the status travels through a field of an error value.
package httpx

import (
	"encoding/json"
	"net/http"
)

type Error struct {
	Status  int
	Code    string
	Message string
}

func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

var ErrInvalidJSON = NewError(http.StatusBadRequest, "invalid_json", "invalid request")

// JSON answers with status and v encoded as JSON.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// WriteError answers with e: {"error": ..., "code": ...}.
func WriteError(w http.ResponseWriter, e *Error) {
	body := map[string]any{"error": e.Message, "code": e.Code}
	JSON(w, e.Status, body)
}

// DecodeJSON reads the body into v, answering 400 itself when it cannot.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		WriteError(w, ErrInvalidJSON)
		return false
	}
	return true
}
