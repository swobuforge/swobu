package exchange

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestFallbackVendor_ExecutesAttemptsUntilSuccess(t *testing.T) {
	ctx := context.Background()
	attempts := []RouteAttempt{
		{Target: RoutableTarget{BackendRef: "backend-a"}},
		{Target: RoutableTarget{BackendRef: "backend-b"}},
	}
	calls := make([]string, 0, len(attempts))

	response, attempt, err := FallbackVendor{CanFallback: func(error) bool { return true }}.Execute(ctx, attempts, func(_ context.Context, attempt RouteAttempt) (TransportResponse, error) {
		calls = append(calls, attempt.Target.BackendRef)
		if attempt.Target.BackendRef == "backend-a" {
			return TransportResponse{}, errors.New("backend-a unavailable")
		}
		return TransportResponse{Progressive: true}, nil
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if attempt.Target.BackendRef != "backend-b" {
		t.Fatalf("attempt backend ref = %q, want backend-b", attempt.Target.BackendRef)
	}
	if !response.Progressive {
		t.Fatal("Execute() response.Progressive = false, want true")
	}
	if !reflect.DeepEqual(calls, []string{"backend-a", "backend-b"}) {
		t.Fatalf("calls = %#v, want backend-a then backend-b", calls)
	}
}

func TestFallbackVendor_StopsOnTerminalError(t *testing.T) {
	ctx := context.Background()
	attempts := []RouteAttempt{
		{Target: RoutableTarget{BackendRef: "backend-a"}},
		{Target: RoutableTarget{BackendRef: "backend-b"}},
	}
	calls := make([]string, 0, len(attempts))

	_, _, err := FallbackVendor{CanFallback: func(error) bool { return false }}.Execute(ctx, attempts, func(_ context.Context, attempt RouteAttempt) (TransportResponse, error) {
		calls = append(calls, attempt.Target.BackendRef)
		return TransportResponse{}, errors.New("terminal failure")
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want terminal failure")
	}
	if !reflect.DeepEqual(calls, []string{"backend-a"}) {
		t.Fatalf("calls = %#v, want only backend-a", calls)
	}
}
