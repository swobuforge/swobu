package provider

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestNormalizeFailureFailsClosedByConcreteClass(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want any
	}{
		{name: "conflict is rejected", err: canonical.NewBackendError("a", http.StatusConflict, "conflict", ""), want: RejectedError{}},
		{name: "payload too large is rejected", err: canonical.NewBackendError("a", http.StatusRequestEntityTooLarge, "large", ""), want: RejectedError{}},
		{name: "unprocessable is rejected", err: canonical.NewBackendError("a", http.StatusUnprocessableEntity, "invalid", ""), want: RejectedError{}},
		{name: "unclassified bad request is rejected", err: canonical.NewBackendError("a", http.StatusBadRequest, "bad", ""), want: RejectedError{}},
		{name: "rate limit is unavailable", err: canonical.NewBackendError("a", http.StatusTooManyRequests, "slow", ""), want: UnavailableError{}},
		{name: "server error is unavailable", err: canonical.NewBackendError("a", http.StatusServiceUnavailable, "down", ""), want: UnavailableError{}},
		{name: "cancellation remains cancellation", err: context.Canceled, want: CancelledError{}},
		{name: "unknown stops as internal", err: errors.New("mystery"), want: InternalError{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeFailure(tc.err)
			switch tc.want.(type) {
			case RejectedError:
				var target RejectedError
				if !errors.As(got, &target) {
					t.Fatalf("error = %T, want RejectedError", got)
				}
			case UnavailableError:
				var target UnavailableError
				if !errors.As(got, &target) {
					t.Fatalf("error = %T, want UnavailableError", got)
				}
			case CancelledError:
				var target CancelledError
				if !errors.As(got, &target) {
					t.Fatalf("error = %T, want CancelledError", got)
				}
			case InternalError:
				var target InternalError
				if !errors.As(got, &target) {
					t.Fatalf("error = %T, want InternalError", got)
				}
			}
		})
	}
}

func TestNormalizeFailurePreservesExplicitUnsupported(t *testing.T) {
	err := NormalizeFailure(NewIncompatibleTarget("unsupported"))
	var unsupported IncompatibleTargetError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %T, want IncompatibleTargetError", err)
	}
}

func TestTransportFailurePreservesInvocationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := TransportFailure(ctx, errors.New("transport stopped"))
	var cancelled CancelledError
	if !errors.As(err, &cancelled) {
		t.Fatalf("error = %T, want CancelledError", err)
	}
	failure, ok := AsAttemptFailure(err)
	if !ok || failure.Execution() != ExecutionMayHaveOccurred {
		t.Fatalf("attempt failure = %#v, %t", failure, ok)
	}
}

func TestBindTransportDefaultsUntypedFailureToPossibleExecution(t *testing.T) {
	target := NewTargetSnapshot("target-a", "openai", "https://example.test", "", "responses", "responses", delivery.BufferedDelivery())
	target.Model = "m"
	transport := BindTransport(target, func(context.Context, TargetSnapshot, carrier.Document) (Ingress, error) {
		return nil, errors.New("connection disappeared")
	})
	_, err := transport.Send(context.Background(), carrier.Document{})
	failure, ok := AsAttemptFailure(err)
	if !ok || failure.Execution() != ExecutionMayHaveOccurred {
		t.Fatalf("attempt failure = %#v, %t", failure, ok)
	}
	var internal InternalError
	if !errors.As(err, &internal) {
		t.Fatalf("cause = %T, want InternalError", err)
	}
}

func TestAttemptFailureConstructionClosesExecutionVocabulary(t *testing.T) {
	cause := Unavailable(errors.New("down"))
	for _, tc := range []struct {
		execution ExecutionPossibility
		construct func(error) AttemptFailure
	}{
		{ExecutionNotDispatched, AttemptNotDispatched},
		{ExecutionRejectedBeforeExecution, AttemptRejectedBeforeExecution},
		{ExecutionMayHaveOccurred, AttemptMayHaveExecuted},
	} {
		failure := tc.construct(cause)
		if failure.Execution() != tc.execution || failure.Cause() == nil {
			t.Fatalf("failure = %#v", failure)
		}
	}
}

func TestIncompatibleTargetRequiresNotDispatchedAttempt(t *testing.T) {
	incompatible := NewIncompatibleTarget("hard caller promise cannot be preserved")
	failure := AttemptNotDispatched(incompatible)
	if failure.Execution() != ExecutionNotDispatched {
		t.Fatalf("execution = %v, want ExecutionNotDispatched", failure.Execution())
	}

	for _, tc := range []struct {
		name      string
		construct func(error) AttemptFailure
	}{
		{name: "rejected before execution", construct: AttemptRejectedBeforeExecution},
		{name: "may have executed", construct: AttemptMayHaveExecuted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid incompatible attempt did not panic")
				}
			}()
			tc.construct(incompatible)
		})
	}
}

func TestIncompatibleTargetInvariantSurvivesAttemptRewrap(t *testing.T) {
	prior := AttemptNotDispatched(NewIncompatibleTarget("hard caller promise cannot be preserved"))
	defer func() {
		if recover() == nil {
			t.Fatal("rewrapped incompatible attempt did not panic")
		}
	}()
	AttemptMayHaveExecuted(prior)
}
