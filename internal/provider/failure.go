package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type classifiedFailure interface {
	error
	providerFailure()
}

// CandidateIncompatibilityError means one exact target cannot represent a valid
// canonical request. Exchange may try another candidate with the unchanged
// request; this type is never a public client error.
type CandidateIncompatibilityError struct {
	Cause error
}

func (e CandidateIncompatibilityError) Error() string {
	if e.Cause == nil {
		return "canonical request is incompatible with provider candidate"
	}
	return fmt.Sprintf("canonical request is incompatible with provider candidate: %v", e.Cause)
}

func (e CandidateIncompatibilityError) Unwrap() error  { return e.Cause }
func (CandidateIncompatibilityError) providerFailure() {}

// CandidateIncompatible retains a typed cause while marking an exact-target
// representation failure.
func CandidateIncompatible(err error) error {
	if err == nil {
		return nil
	}
	return CandidateIncompatibilityError{Cause: err}
}

// NewCandidateIncompatibility marks an exact-target representation failure
// without constructing or wrapping a public Swobu error.
func NewCandidateIncompatibility(message string) error {
	return CandidateIncompatible(errors.New(message))
}

// UnavailableError means provider I/O could not produce a usable backend
// response. Another configured backend may be attempted while fallback is
// otherwise open.
type UnavailableError struct{ Cause error }

func (e UnavailableError) Error() string {
	return failureMessage("provider backend is unavailable", e.Cause)
}
func (e UnavailableError) Unwrap() error  { return e.Cause }
func (UnavailableError) providerFailure() {}

// RejectedError means the backend returned a response rejecting this request.
// Rejection is terminal unless the adapter has positively identified the
// narrower CandidateIncompatibilityError contract.
type RejectedError struct{ Cause error }

func (e RejectedError) Error() string {
	return failureMessage("provider backend rejected the request", e.Cause)
}
func (e RejectedError) Unwrap() error  { return e.Cause }
func (RejectedError) providerFailure() {}

// InvalidRequestError means provider execution could not begin because the
// selected request or backend configuration is invalid.
type InvalidRequestError struct{ Cause error }

func (e InvalidRequestError) Error() string {
	return failureMessage("provider request is invalid", e.Cause)
}
func (e InvalidRequestError) Unwrap() error  { return e.Cause }
func (InvalidRequestError) providerFailure() {}

// CancelledError means invocation cancellation stopped provider execution.
type CancelledError struct{ Cause error }

func (e CancelledError) Error() string {
	return failureMessage("provider request was cancelled", e.Cause)
}
func (e CancelledError) Unwrap() error  { return e.Cause }
func (CancelledError) providerFailure() {}

// InternalError means provider execution failed without a safe routing
// classification. Unknown errors fail closed as internal.
type InternalError struct{ Cause error }

func (e InternalError) Error() string {
	return failureMessage("provider execution failed internally", e.Cause)
}
func (e InternalError) Unwrap() error  { return e.Cause }
func (InternalError) providerFailure() {}

func Unavailable(err error) error {
	return wrapFailure(err, func(cause error) error { return UnavailableError{Cause: cause} })
}
func Rejected(err error) error {
	return wrapFailure(err, func(cause error) error { return RejectedError{Cause: cause} })
}
func InvalidRequest(err error) error {
	return wrapFailure(err, func(cause error) error { return InvalidRequestError{Cause: cause} })
}
func Cancelled(err error) error {
	return wrapFailure(err, func(cause error) error { return CancelledError{Cause: cause} })
}
func Internal(err error) error {
	return wrapFailure(err, func(cause error) error { return InternalError{Cause: cause} })
}

// TransportFailure classifies a failed provider I/O attempt without losing
// invocation cancellation behind a generic availability wrapper.
func TransportFailure(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return Cancelled(ctx.Err())
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Cancelled(err)
	}
	return Unavailable(err)
}

// NormalizeFailure closes the backend transport error vocabulary. Exact
// adapters may return a narrower classified failure; unclassified values stop
// by default and never become fallback-eligible through status inference in
// exchange orchestration.
func NormalizeFailure(err error) error {
	if err == nil {
		return nil
	}
	var classified classifiedFailure
	if errors.As(err, &classified) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Cancelled(err)
	}
	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		switch status := backendErr.StatusCode; {
		case status == http.StatusRequestTimeout,
			status == http.StatusTooEarly,
			status == http.StatusTooManyRequests,
			status >= http.StatusInternalServerError && status <= 599:
			return Unavailable(err)
		case status >= http.StatusBadRequest:
			return Rejected(err)
		default:
			return Internal(err)
		}
	}
	var canonicalErr canonical.Error
	if errors.As(err, &canonicalErr) {
		if canonicalErr.Code == canonical.ErrorCodeInternal {
			return Internal(err)
		}
		return InvalidRequest(err)
	}
	return Internal(err)
}

func wrapFailure(err error, wrap func(error) error) error {
	if err == nil {
		return nil
	}
	return wrap(err)
}

func failureMessage(summary string, cause error) string {
	if cause == nil {
		return summary
	}
	return fmt.Sprintf("%s: %v", summary, cause)
}
