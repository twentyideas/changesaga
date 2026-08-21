package reviewapp

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	CodeInvalidArgument   ErrorCode = "invalid_argument"
	CodeNotFound          ErrorCode = "not_found"
	CodeInvalidSaga       ErrorCode = "invalid_saga"
	CodeSourceUnavailable ErrorCode = "source_unavailable"
	CodeStaleSnapshot     ErrorCode = "stale_snapshot"
	CodeConflict          ErrorCode = "conflict"
	CodeUnsafePath        ErrorCode = "unsafe_path"
	CodeUnsupportedMedia  ErrorCode = "unsupported_media"
	CodeTooLarge          ErrorCode = "too_large"
	CodeInternal          ErrorCode = "internal"
)

// Error is a stable domain error. Cause is retained for logs but intentionally
// excluded from serialized output because it can contain local absolute paths.
type Error struct {
	Code      ErrorCode      `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
	cause     error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.cause }

func newError(code ErrorCode, message string, retryable bool, details map[string]any, cause error) error {
	return &Error{Code: code, Message: message, Retryable: retryable, Details: details, cause: cause}
}

func invalidArgument(message string) error {
	return newError(CodeInvalidArgument, message, false, nil, nil)
}

func notFound(kind, id string) error {
	return newError(CodeNotFound, fmt.Sprintf("%s was not found", kind), false, map[string]any{"kind": kind, "id": id}, nil)
}

// ErrorCodeOf returns the stable code for a domain error. Unexpected errors
// deliberately collapse to internal rather than exposing implementation text.
func ErrorCodeOf(err error) ErrorCode {
	var domain *Error
	if errors.As(err, &domain) {
		return domain.Code
	}
	return CodeInternal
}

func AsError(err error) *Error {
	var domain *Error
	if errors.As(err, &domain) {
		return domain
	}
	if err == nil {
		return nil
	}
	return &Error{Code: CodeInternal, Message: "an unexpected error occurred", Retryable: false, cause: err}
}
