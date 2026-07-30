package wire

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

func TestResponseCompletionIsWriteOnceAndNonBlocking(t *testing.T) {
	observation, complete, fail := NewResponseCompletion()
	if got := observation.Snapshot(); got.State != CompletionPending {
		t.Fatalf("initial state = %v, want pending", got.State)
	}
	fingerprint, err := historyfingerprint.FingerprintResponse("responses", []byte("response"))
	if err != nil {
		t.Fatal(err)
	}
	complete(&fingerprint, nil)
	fail(errors.New("late failure"))
	got := observation.Snapshot()
	if got.State != CompletionCompleted || got.ResponseFingerprint == nil || *got.ResponseFingerprint != fingerprint || got.Err != nil {
		t.Fatalf("completed snapshot = %#v", got)
	}
}

func TestResponseCompletionFailureCarriesNoFingerprint(t *testing.T) {
	cell, complete, fail := NewResponseCompletion()
	want := errors.New("encode failed")
	fail(want)
	fingerprint, err := historyfingerprint.FingerprintResponse("responses", []byte("late"))
	if err != nil {
		t.Fatal(err)
	}
	complete(&fingerprint, nil)
	got := cell.Snapshot()
	if got.State != CompletionFailed || !errors.Is(got.Err, want) || got.ResponseFingerprint != nil {
		t.Fatalf("failed snapshot = %#v", got)
	}
}

func TestResponseCompletionCanCompleteWithoutFingerprintCapability(t *testing.T) {
	cell, complete, _ := NewResponseCompletion()
	complete(nil, nil)
	got := cell.Snapshot()
	if got.State != CompletionCompleted || got.ResponseFingerprint != nil || got.Err != nil {
		t.Fatalf("completed snapshot without capability = %#v", got)
	}
}
