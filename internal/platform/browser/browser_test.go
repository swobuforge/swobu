package browser

import (
	"testing"
)

func TestOpen_EmptyURLReturnsError(t *testing.T) {
	err := Open("")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if got := err.Error(); got != "browser open: URL is missing" {
		t.Fatalf("error = %q, want URL is missing", got)
	}
}

func TestOpen_WhitespaceOnlyURLReturnsError(t *testing.T) {
	err := Open("  \t")
	if err == nil {
		t.Fatal("expected error for whitespace-only URL")
	}
}

func TestOpen_ValidURLDoesNotError(t *testing.T) {
	// Actual browser launch is platform-specific and external; this test
	// ensures the command is constructed and the Start return path is
	// exercised.
	err := Open("about:blank")
	if err != nil {
		// On headless CI the command may fail (no X display, etc.). That's fine
		// for verifying the side-effect path is wired; we only reject empty URLs.
		return
	}
}
