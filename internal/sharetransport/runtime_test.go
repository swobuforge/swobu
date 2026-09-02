package sharetransport

import (
	"errors"
	"sync"
	"testing"
)

func TestReadinessResultBroadcastsOneFailureToEveryWaiter(t *testing.T) {
	result := newReadinessResult()
	want := errors.New("certificate failed")
	const waiters = 8
	observed := make(chan error, waiters)
	var started sync.WaitGroup
	started.Add(waiters)
	for range waiters {
		go func() { started.Done(); <-result.done; observed <- result.err }()
	}
	started.Wait()
	result.complete(want)
	for range waiters {
		if got := <-observed; !errors.Is(got, want) {
			t.Fatalf("waiter error = %v", got)
		}
	}
	result.complete(errors.New("replacement"))
	if !errors.Is(result.err, want) {
		t.Fatalf("terminal result changed: %v", result.err)
	}
}
