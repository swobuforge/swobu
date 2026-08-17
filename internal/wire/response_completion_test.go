package wire

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

func TestResponseCompletionPreservesCacheUsagePresence(t *testing.T) {
	cell, complete, _ := NewResponseCompletion()
	zero := 0
	write := 7
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{CacheReadTokens: &zero, CacheWriteTokens: &write})
	if err != nil {
		t.Fatal(err)
	}
	cell.ObserveUsage(usage)
	complete(nil, nil)
	got := cell.Snapshot().Usage
	if value, ok := got.CacheReadTokens(); !ok || value != 0 {
		t.Fatalf("cache read = %d, %t", value, ok)
	}
	if value, ok := got.CacheWriteTokens(); !ok || value != 7 {
		t.Fatalf("cache write = %d, %t", value, ok)
	}
}

func TestResponseCompletionIsWriteOnceAndNonBlocking(t *testing.T) {
	observation, complete, fail := NewResponseCompletion()
	if got := observation.Snapshot(); got.State != CompletionPending {
		t.Fatalf("initial state = %v, want pending", got.State)
	}
	fingerprint, err := historyfingerprint.FingerprintResponse("responses/v1", []byte("response"))
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
	fingerprint, err := historyfingerprint.FingerprintResponse("responses/v1", []byte("late"))
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
