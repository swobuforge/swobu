package testharness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssertVisual_MatchesFixture(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "screen.txt")
	if err := os.WriteFile(fixture, []byte("hello\nworld"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	result := AssertVisual("matches_fixture").Fixture(fixture).Against("hello\nworld")
	if result.Err != nil {
		t.Fatalf("unexpected visual mismatch: %v\n%s", result.Err, result.Diff)
	}
}

func TestAssertVisual_NowMismatchProducesDiff(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "screen.txt")
	if err := os.WriteFile(fixture, []byte("expected line\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	err := AssertVisual("mismatch_now").Fixture(fixture).Now("actual line\n")
	if err == nil {
		t.Fatal("expected error for visual mismatch")
	}
	got := err.Error()
	if !strings.Contains(got, "visual mismatch fixture=") {
		t.Fatalf("error missing fixture ref: %v", got)
	}
}

func TestAssertVisual_MissingFixtureHasPromoteGuidance(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "not-there.txt")

	result := AssertVisual("missing_fixture").Fixture(missing).Against("snapshot\n")
	if result.Err == nil {
		t.Fatal("expected error for missing fixture")
	}
	got := result.Err.Error()
	if !strings.Contains(got, "missing visual fixture") || !strings.Contains(got, "SWOBU_UPDATE_WIREFRAMES=1") {
		t.Fatalf("missing helpful guidance in error: %v", got)
	}
}

func TestAssertVisual_UpdatesFixtureWhenEnabled(t *testing.T) {
	t.Setenv(updateWireframesEnv, "1")
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fresh.txt")

	err := AssertVisual("update_fixture").Fixture(fixture).Now("fresh snapshot\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("fixture should not be empty after promotion")
	}
}

func TestAssertVisual_DerivesFixturePath(t *testing.T) {
	report := AssertVisual("Ready Screen").Viewport(80, 24).Against("any")
	if report.Err != nil {
		// Missing fixture is expected here; we only care about the derived path.
		if !strings.Contains(report.Err.Error(), "missing visual fixture") {
			t.Fatalf("unexpected error: %v", report.Err)
		}
	}
	if report.FixturePath == "" {
		t.Fatal("derived fixture path should not be empty")
	}
	if !strings.Contains(report.FixturePath, "ready_screen") {
		t.Fatalf("fixture path should contain asserted name: %q", report.FixturePath)
	}
}
