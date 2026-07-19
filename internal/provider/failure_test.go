package provider

import (
	"context"
	"errors"
	"net/http"
	"testing"

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
	err := NormalizeFailure(UnsupportedByBackend(errors.New("unsupported")))
	var unsupported UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %T, want UnsupportedError", err)
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
}
