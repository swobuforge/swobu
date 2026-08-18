package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type classifiedFailure interface {
	error
	providerFailure()
}

// ExecutionPossibility records only what Swobu can prove about one issued
// provider call. It is independent of the provider failure class.
type ExecutionPossibility uint8

const (
	ExecutionNotDispatched ExecutionPossibility = iota + 1
	ExecutionRejectedBeforeExecution
	ExecutionMayHaveOccurred
)

// AttemptFailure is the validated terminal fact for one issued provider call.
// Bound transports conservatively wrap an untyped error as
// ExecutionMayHaveOccurred; only the exact adapter may claim an earlier fact.
type AttemptFailure struct {
	execution ExecutionPossibility
	cause     error
}

func (e AttemptFailure) Error() string { return e.cause.Error() }
func (e AttemptFailure) Unwrap() error { return e.cause }

func (e AttemptFailure) Execution() ExecutionPossibility { return e.execution }
func (e AttemptFailure) Cause() error                    { return e.cause }

func (e AttemptFailure) valid() bool {
	return e.cause != nil &&
		(e.execution == ExecutionNotDispatched ||
			e.execution == ExecutionRejectedBeforeExecution ||
			e.execution == ExecutionMayHaveOccurred)
}

func newAttemptFailure(execution ExecutionPossibility, cause error) AttemptFailure {
	if cause == nil {
		panic("provider attempt failure requires a cause")
	}
	if prior, ok := AsAttemptFailure(cause); ok {
		cause = prior.Cause()
	}
	failure := AttemptFailure{execution: execution, cause: normalizeFailureCause(cause)}
	if !failure.valid() {
		panic("provider attempt failure has invalid execution possibility")
	}
	return failure
}

func AttemptNotDispatched(cause error) AttemptFailure {
	return newAttemptFailure(ExecutionNotDispatched, cause)
}

func AttemptRejectedBeforeExecution(cause error) AttemptFailure {
	return newAttemptFailure(ExecutionRejectedBeforeExecution, cause)
}

func AttemptMayHaveExecuted(cause error) AttemptFailure {
	return newAttemptFailure(ExecutionMayHaveOccurred, cause)
}

// AsAttemptFailure extracts a validated issued-call fact.
func AsAttemptFailure(err error) (AttemptFailure, bool) {
	var failure AttemptFailure
	if !errors.As(err, &failure) || !failure.valid() {
		return AttemptFailure{}, false
	}
	return failure, true
}

// IncompatibleTargetError means one exact target cannot represent a valid
// canonical request. Exchange may try another candidate with the unchanged
// request; this type is never a public client error.
type IncompatibleTargetError struct {
	Reason string
	cause  error
}

func (e IncompatibleTargetError) Error() string {
	if e.Reason == "" {
		return "canonical request is incompatible with provider candidate"
	}
	return fmt.Sprintf("canonical request is incompatible with provider candidate: %s", e.Reason)
}

func (IncompatibleTargetError) providerFailure() {}
func (e IncompatibleTargetError) Unwrap() error  { return e.cause }

// IncompatibleTarget retains a typed cause while marking an exact-target
// representation failure.
func IncompatibleTarget(err error) error {
	if err == nil {
		return nil
	}
	return IncompatibleTargetError{Reason: err.Error(), cause: err}
}

// NewIncompatibleTarget marks an exact-target representation failure
// without constructing or wrapping a public Swobu error.
func NewIncompatibleTarget(message string) error {
	return IncompatibleTargetError{Reason: message}
}

// IncompatibleCapability marks one concrete canonical occurrence that the
// selected target cannot lower. It preserves bounded human detail without
// creating a provider support registry or allowing the issue to choose routing.
func IncompatibleCapability(capability canonical.CapabilityPath, occurrence canonical.Occurrence, detail string) error {
	unsupported := compat.NewUnsupported(compat.NewIssue(capability, occurrence))
	if detail == "" {
		return IncompatibleTarget(unsupported)
	}
	return IncompatibleTarget(fmt.Errorf("%s: %w", detail, unsupported))
}

// UnavailableError means provider I/O could not produce a usable backend
// response. It does not state whether provider execution occurred or whether
// replay is safe.
type UnavailableError struct{ Cause error }

func (e UnavailableError) Error() string {
	return failureMessage("provider backend is unavailable", e.Cause)
}
func (e UnavailableError) Unwrap() error  { return e.Cause }
func (UnavailableError) providerFailure() {}

// RejectedError means the backend returned a response rejecting this exact
// target. A complete 4xx is target-local evidence; it does not prove canonical
// invalidity or absence of execution. Exact-target rejection is combined with
// execution possibility and replay safety by the route reducer, so this type is
// never itself a public client error and never grants replay.
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
		return AttemptMayHaveExecuted(Cancelled(ctx.Err()))
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return AttemptMayHaveExecuted(Cancelled(err))
	}
	return AttemptMayHaveExecuted(Unavailable(err))
}

// RetryNotBefore extracts the standard backend retry timing fact without
// choosing routing or backoff policy. Invalid, negative, expired, and absent
// hints are ignored.
func RetryNotBefore(err error, now time.Time) (time.Time, bool) {
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		return time.Time{}, false
	}
	raw := strings.TrimSpace(backendErr.RetryAfterHeaderValue)
	if raw == "" {
		return time.Time{}, false
	}
	if seconds, parseErr := strconv.ParseUint(raw, 10, 63); parseErr == nil {
		if seconds > uint64((time.Duration(1<<63-1))/time.Second) {
			return time.Time{}, false
		}
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	deadline, parseErr := http.ParseTime(raw)
	if parseErr != nil || !deadline.After(now) {
		return time.Time{}, false
	}
	return deadline, true
}

// NormalizeFailure closes the provider cause vocabulary. Execution possibility
// remains a separate AttemptFailure fact; this function never grants replay.
func NormalizeFailure(err error) error {
	if err == nil {
		return nil
	}
	if failure, ok := AsAttemptFailure(err); ok {
		return failure
	}
	return normalizeFailureCause(err)
}

func normalizeFailureCause(err error) error {
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
