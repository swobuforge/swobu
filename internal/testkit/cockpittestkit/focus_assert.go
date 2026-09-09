package testkit

import (
	"strings"
	"testing"
)

// AssertFocusedFrame proves that a rendered frame already contains the
// expected focus marker. Use this when the test has already driven the app to
// the target state and only needs the visible contract.
func AssertFocusedFrame(t testing.TB, frame, want string) {
	t.Helper()

	if !strings.Contains(frame, want) {
		t.Fatalf("focused frame missing %q:\n%s", want, frame)
	}
}

// AssertUnfocusedFrame proves that a rendered frame does not yet contain the
// expected focus marker. It is the negative counterpart to AssertFocusedFrame.
func AssertUnfocusedFrame(t testing.TB, frame, want string) {
	t.Helper()

	if strings.Contains(frame, want) {
		t.Fatalf("unfocused frame unexpectedly contained %q:\n%s", want, frame)
	}
}

// AssertFocusVisible proves that a focus interaction changes the rendered frame
// and exposes the expected focused marker. This is a test-stage contract, not a
// runtime panic path.
func AssertFocusVisible(t testing.TB, h *MockAppHarness, step func(), want string) {
	t.Helper()

	before := h.Frame()
	step()
	after := h.Frame()

	if before == after {
		t.Fatalf("focus interaction did not change rendered frame; expected focused output to contain %q\nframe:\n%s", want, after)
	}
	AssertFocusedFrame(t, after, want)
}
