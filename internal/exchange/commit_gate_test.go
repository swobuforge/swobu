package exchange

import "testing"

func TestCommitGate_TracksFallbackAvailability(t *testing.T) {
	gate := NewCommitGate()
	if !gate.CanFallback() {
		t.Fatal("new commit gate should allow fallback")
	}
	if gate.Committed() {
		t.Fatal("new commit gate should not report committed")
	}

	gate.Commit()

	if gate.CanFallback() {
		t.Fatal("committed gate should not allow fallback")
	}
	if !gate.Committed() {
		t.Fatal("committed gate should report committed")
	}
}
