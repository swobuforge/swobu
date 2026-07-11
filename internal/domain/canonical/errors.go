package canonical

import (
	"errors"
	"fmt"
)

type ErrorOrigin string

type ErrorCode string

const (
	ErrorCodeInternal             ErrorCode = "INTERNAL_ERROR"
	ErrorCodeUnsupportedEndpoint  ErrorCode = "UNSUPPORTED_ENDPOINT"
	ErrorCodeUnsupportedOperation ErrorCode = "UNSUPPORTED_OPERATION"
	ErrorCodeUnsupportedDelivery  ErrorCode = "UNSUPPORTED_DELIVERY_MODE"
	ErrorCodeBadEndpoint          ErrorCode = "BAD_ENDPOINT"
	ErrorCodeBadRequest           ErrorCode = "BAD_REQUEST"
	ErrorCodeUnknownTarget        ErrorCode = "UNKNOWN_TARGET"
)

const (
	ErrorOriginSwobu   ErrorOrigin = "swobu"
	ErrorOriginBackend ErrorOrigin = "backend"
)

type Error struct {
	Code    ErrorCode
	Message string
	Origin  ErrorOrigin
	Details map[string]string
}

func (e Error) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewSwobuError builds a typed Swobu-originated failure for the protocol contract.
func NewSwobuError(code ErrorCode, message string) Error {
	return Error{
		Code:    code,
		Message: message,
		Origin:  ErrorOriginSwobu,
	}
}

func UnsupportedEndpoint(message string) Error {
	return NewSwobuError(ErrorCodeUnsupportedEndpoint, message)
}

func InternalError(message string) Error {
	return NewSwobuError(ErrorCodeInternal, message)
}

func BadEndpoint(message string) Error {
	return NewSwobuError(ErrorCodeBadEndpoint, message)
}

func BadRequest(message string) Error {
	return NewSwobuError(ErrorCodeBadRequest, message)
}

func UnsupportedOperation(message string) Error {
	return NewSwobuError(ErrorCodeUnsupportedOperation, message)
}

func UnsupportedDelivery(message string) Error {
	return NewSwobuError(ErrorCodeUnsupportedDelivery, message)
}

type BackendError struct {
	Origin     ErrorOrigin
	BackendRef string
	StatusCode int
	Message    string
	// RetryAfterHeaderValue is the only allowed backend-header passthrough in v0.
	// Keep this narrow field explicit instead of introducing a generic header map.
	RetryAfterHeaderValue string
}

type BackendErrorClass string

const BackendErrorClassToolChoiceUnsupported BackendErrorClass = "tool_choice_unsupported"

// ClassifiedBackendError preserves backend-origin error truth while carrying a
// provider-edge capability classification derived from raw backend envelopes.
type ClassifiedBackendError struct {
	Class BackendErrorClass
	Cause BackendError
}

func NewClassifiedBackendError(class BackendErrorClass, cause BackendError) ClassifiedBackendError {
	return ClassifiedBackendError{
		Class: class,
		Cause: cause,
	}
}

func (e ClassifiedBackendError) Error() string {
	if e.Cause.Origin == "" && e.Cause.BackendRef == "" && e.Cause.Message == "" && e.Cause.StatusCode == 0 {
		return "backend classified error"
	}
	return e.Cause.Error()
}

func (e ClassifiedBackendError) Unwrap() error {
	return e.Cause
}

// NewBackendError preserves backend-origin truth instead of laundering provider
// failures into Swobu-shaped validation or policy errors.
func NewBackendError(backendRef string, statusCode int, message string, retryAfterHeaderValue string) BackendError {
	return BackendError{
		Origin:                ErrorOriginBackend,
		BackendRef:            backendRef,
		StatusCode:            statusCode,
		Message:               message,
		RetryAfterHeaderValue: retryAfterHeaderValue,
	}
}

func (e BackendError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("backend error from %s (%d)", e.BackendRef, e.StatusCode)
	}
	return fmt.Sprintf("backend error from %s (%d): %s", e.BackendRef, e.StatusCode, e.Message)
}

func IsBackendErrorClass(err error, class BackendErrorClass) bool {
	var classifiedErr ClassifiedBackendError
	if !errors.As(err, &classifiedErr) {
		return false
	}
	return classifiedErr.Class == class
}
