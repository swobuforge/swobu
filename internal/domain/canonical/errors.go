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
	ErrorCodeNotImplemented       ErrorCode = "NOT_IMPLEMENTED"
	ErrorCodeNoCompatibleTarget   ErrorCode = "NO_COMPATIBLE_TARGET"
)

const (
	ErrorOriginSwobu   ErrorOrigin = "swobu"
	ErrorOriginBackend ErrorOrigin = "backend"
)

// ValidErrorCode reports whether c is a canonical error code. It is the strict
// membership test the terminal-event constructor uses to reject a typed-but-
// non-canonical value at the source. An empty code ("no canonical error") is not
// itself a code; callers gate it separately.
func ValidErrorCode(c ErrorCode) bool {
	switch c {
	case ErrorCodeInternal, ErrorCodeUnsupportedEndpoint, ErrorCodeUnsupportedOperation, ErrorCodeUnsupportedDelivery, ErrorCodeBadEndpoint, ErrorCodeBadRequest, ErrorCodeUnknownTarget, ErrorCodeNotImplemented, ErrorCodeNoCompatibleTarget:
		return true
	}
	return false
}

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

// newSwobuError builds a typed Swobu-originated failure for the protocol contract.
func newSwobuError(code ErrorCode, message string) Error {
	return Error{
		Code:    code,
		Message: message,
		Origin:  ErrorOriginSwobu,
	}
}

func UnsupportedEndpoint(message string) Error {
	return newSwobuError(ErrorCodeUnsupportedEndpoint, message)
}

func InternalError(message string) Error {
	return newSwobuError(ErrorCodeInternal, message)
}

func BadEndpoint(message string) Error {
	return newSwobuError(ErrorCodeBadEndpoint, message)
}

func BadRequest(message string) Error {
	return newSwobuError(ErrorCodeBadRequest, message)
}

// ClientUnsupportedOperation reports a caller-controlled public operation that
// can be changed without changing the user's intent. retryChange is included in
// the public message so the caller receives the action that makes the request
// supportable.
func ClientUnsupportedOperation(message, retryChange string) Error {
	if message == "" {
		message = "the requested public operation is unsupported"
	}
	if retryChange == "" {
		return InternalError("client unsupported operation is missing its retry change")
	}
	return newSwobuError(ErrorCodeUnsupportedOperation, message+". "+retryChange)
}

// NotImplemented reports valid or forward-valid protocol semantics whose
// required codec or canonical path is absent from Swobu.
func NotImplemented(message string) Error {
	return newSwobuError(ErrorCodeNotImplemented, message)
}

// NoCompatibleTarget reports that every configured candidate rejected the
// unchanged canonical request as unrepresentable.
func NoCompatibleTarget(message string) Error {
	return newSwobuError(ErrorCodeNoCompatibleTarget, message)
}

func UnknownTarget(message string) Error {
	return newSwobuError(ErrorCodeUnknownTarget, message)
}

// ClientUnsupportedDelivery reports a caller-controlled delivery mode with a
// concrete supported alternative.
func ClientUnsupportedDelivery(message, retryChange string) Error {
	if message == "" {
		message = "the requested delivery mode is unsupported"
	}
	if retryChange == "" {
		return InternalError("client unsupported delivery is missing its retry change")
	}
	return newSwobuError(ErrorCodeUnsupportedDelivery, message+". "+retryChange)
}

type BackendError struct {
	Origin     ErrorOrigin
	TargetID   string
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
	if e.Cause.Origin == "" && e.Cause.TargetID == "" && e.Cause.Message == "" && e.Cause.StatusCode == 0 {
		return "backend classified error"
	}
	return e.Cause.Error()
}

func (e ClassifiedBackendError) Unwrap() error {
	return e.Cause
}

// NewBackendError preserves backend-origin truth instead of laundering provider
// failures into Swobu-shaped validation or policy errors.
func NewBackendError(targetID string, statusCode int, message string, retryAfterHeaderValue string) BackendError {
	return BackendError{
		Origin:                ErrorOriginBackend,
		TargetID:              targetID,
		StatusCode:            statusCode,
		Message:               message,
		RetryAfterHeaderValue: retryAfterHeaderValue,
	}
}

func (e BackendError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("backend error from %s (%d)", e.TargetID, e.StatusCode)
	}
	return fmt.Sprintf("backend error from %s (%d): %s", e.TargetID, e.StatusCode, e.Message)
}

func IsBackendErrorClass(err error, class BackendErrorClass) bool {
	var classifiedErr ClassifiedBackendError
	if !errors.As(err, &classifiedErr) {
		return false
	}
	return classifiedErr.Class == class
}
