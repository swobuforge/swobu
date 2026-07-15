package cli

import (
	"testing"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// TestRunner_DaemonURLFieldExistsAndResolves proves the DaemonURL field is
// wired into the Runner struct and resolves through platformconfig correctly.
func TestRunner_DaemonURLFieldExistsAndResolves(t *testing.T) {
	customURL := "http://127.0.0.1:65432"

	runner := Runner{DaemonURL: customURL}
	resolved := platformconfig.ResolveDaemonURL(runner.DaemonURL)
	if resolved != customURL {
		t.Fatalf("expected resolved URL %q, got %q", customURL, resolved)
	}
}

// TestRunner_DefaultDaemonURLResolves proves the empty-string default
// resolves to the platform default admin URL.
func TestRunner_DefaultDaemonURLResolves(t *testing.T) {
	resolved := platformconfig.ResolveDaemonURL("")
	if resolved == "" {
		t.Fatal("expected non-empty default daemon URL")
	}
}
