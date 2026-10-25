package relaytransport

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"github.com/swobuforge/swobu/internal/relaystate"
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

func TestPublicWorkspaceReadinessLeasePreventsConcurrentInactiveStop(t *testing.T) {
	store, err := relaystate.Open(filepath.Join(t.TempDir(), "relay.json"))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewOwnerRuntime("unused.invalid:443", nil, store, &relaystate.TLSManager{Store: store}, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Stop()

	ready := newReadinessResult()
	ready.complete(nil)
	runtime.running = true
	runtime.readiness = ready

	lease, err := runtime.EnsurePublicWorkspaceLease(context.Background())
	if err != nil {
		t.Fatal("Owner readiness lease failed", err)
	}
	// Publication has not committed yet; this temporary lease keeps relay active.
	runtime.StopIfInactive()
	if !runtime.running {
		t.Fatal("concurrent inactive reevaluation stopped Owner before publication commit")
	}
	lease.Release()
	if runtime.running {
		t.Fatal("Owner remained active after its final readiness lease was released")
	}
}
