package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
)

// APIError is the single error shape every endpoint returns, so that clients
// have exactly one thing to parse.
type APIError struct {
	// Status is the HTTP status code; it is not serialised.
	Status int `json:"-"`
	// Code is a stable machine readable identifier such as "not_found".
	Code string `json:"code"`
	// Message is a sentence a person can read and act on.
	Message string `json:"message"`
	// Fields carries per-field validation messages keyed by field name.
	Fields map[string]string `json:"fields,omitempty"`
	// RequestID lets a user quote something we can find in the logs.
	RequestID string `json:"requestId,omitempty"`

	// cause is logged but never sent to the client.
	cause error
}

func (e *APIError) Error() string {
	if e.cause != nil {
		return e.Code + ": " + e.Message + ": " + e.cause.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *APIError) Unwrap() error { return e.cause }

// errorEnvelope is how every failure is written.
type errorEnvelope struct {
	Error APIError `json:"error"`
}

func ErrBadRequest(message string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Code: "bad_request", Message: message}
}

func ErrValidation(fields map[string]string) *APIError {
	return &APIError{
		Status:  http.StatusUnprocessableEntity,
		Code:    "validation_failed",
		Message: "Some fields need attention.",
		Fields:  fields,
	}
}

func ErrNotFound(message string) *APIError {
	if message == "" {
		message = "There is nothing at this address."
	}
	return &APIError{Status: http.StatusNotFound, Code: "not_found", Message: message}
}

func ErrMethodNotAllowed() *APIError {
	return &APIError{
		Status:  http.StatusMethodNotAllowed,
		Code:    "method_not_allowed",
		Message: "This address does not answer that method.",
	}
}

func ErrUnavailable(message string) *APIError {
	return &APIError{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: message}
}

// ErrInternal hides the cause from the client and keeps it for the log.
func ErrInternal(cause error) *APIError {
	return &APIError{
		Status:  http.StatusInternalServerError,
		Code:    "internal",
		Message: "Something went wrong on the server. The request ID below identifies it in the logs.",
		cause:   cause,
	}
}

// writeError sends any error as the envelope. What is not an APIError is a
// failure nobody anticipated, so the client learns nothing about it.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		apiErr = ErrInternal(err)
	}
	if apiErr.Status >= http.StatusInternalServerError {
		loggerFrom(r.Context()).Error("request failed", "error", apiErr.Error())
	}
	sent := *apiErr
	sent.RequestID = RequestIDFrom(r.Context())
	writeJSON(w, r, apiErr.Status, errorEnvelope{Error: sent})
}

// writeJSON sends a value as the response body. Headers are out once encoding
// starts, so a failure here can only be logged.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		loggerFrom(r.Context()).Warn("response not written", "error", err)
	}
}
