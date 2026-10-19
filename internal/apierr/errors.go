// Package apierr defines structured error codes and helpers for consistent
// API error responses across REST and JSON-RPC endpoints.
package apierr

import (
	"fmt"
	"net/http"
)

// Code is a machine-readable error identifier.
type Code string

const (
	CodeBadRequest        Code = "BAD_REQUEST"
	CodeUnauthorized      Code = "UNAUTHORIZED"
	CodeForbidden         Code = "FORBIDDEN"
	CodeNotFound          Code = "NOT_FOUND"
	CodeRateLimited       Code = "RATE_LIMITED"
	CodeTimeout           Code = "TOOL_TIMEOUT"
	CodeInternalError     Code = "INTERNAL_ERROR"
	CodeParseError        Code = "PARSE_ERROR"
	CodeExecutionError    Code = "EXECUTION_ERROR"
	CodeUnsupportedLang   Code = "UNSUPPORTED_LANGUAGE"
	CodeFileTooLarge      Code = "FILE_TOO_LARGE"
	CodeInvalidExpression Code = "INVALID_EXPRESSION"
	CodePermissionDenied  Code = "PERMISSION_DENIED"
)

// APIError is the canonical structured error returned by all endpoints.
type APIError struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	// HTTPStatus is NOT marshalled — it is used to set the response status code.
	HTTPStatus int `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// New creates an APIError with an associated HTTP status code.
func New(code Code, httpStatus int, msg string) *APIError {
	return &APIError{Code: code, HTTPStatus: httpStatus, Message: msg}
}

// Newf creates a formatted APIError.
func Newf(code Code, httpStatus int, format string, args ...any) *APIError {
	return &APIError{Code: code, HTTPStatus: httpStatus, Message: fmt.Sprintf(format, args...)}
}

// WithDetail attaches a detail string to an existing APIError (fluent).
func (e *APIError) WithDetail(detail string) *APIError {
	e.Detail = detail
	return e
}

// Pre-built common errors (zero-allocation singletons — callers must NOT mutate).
var (
	ErrUnauthorized  = New(CodeUnauthorized, http.StatusUnauthorized, "missing or invalid bearer token")
	ErrForbidden     = New(CodeForbidden, http.StatusForbidden, "insufficient permissions for this tool")
	ErrRateLimited   = New(CodeRateLimited, http.StatusTooManyRequests, "rate limit exceeded — back off and retry")
	ErrInternalError = New(CodeInternalError, http.StatusInternalServerError, "an unexpected internal error occurred")
)
