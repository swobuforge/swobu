package fixture

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPath_BuildsCanonicalVisualFixturePath(t *testing.T) {
	got := Path("default_launch__testdefaultlaunch", "Ready Screen")
	want := filepath.Join("testdata", "default_launch__testdefaultlaunch", "fixture", "ready_screen.txt")
	if got != want {
		t.Fatalf("Path()=%q want %q", got, want)
	}
}

func TestPath_EmptyAssertionUsesDefault(t *testing.T) {
	got := Path("cockpit__testrenderdefaultworkspace", "")
	want := filepath.Join("testdata", "cockpit__testrenderdefaultworkspace", "fixture", "default.txt")
	if got != want {
		t.Fatalf("Path()=%q want %q", got, want)
	}
}

func TestCompareSnapshot_UsesSharedUpdateEnv(t *testing.T) {
	t.Setenv(UpdateEnv, "1")
	cfg := ConfigForIn(t.TempDir(), "screen", "default")
	report := CompareSnapshot("fresh\n", cfg)
	if report.Err != nil {
		t.Fatalf("CompareSnapshot() unexpected error: %v", report.Err)
	}
	if !strings.HasSuffix(report.FixturePath, filepath.Join("screen", "fixture", "default.txt")) {
		t.Fatalf("FixturePath=%q", report.FixturePath)
	}
}

func TestBuilderFor_UsesSharedVisualDefaults(t *testing.T) {
	cfg := BuilderFor("screen__testready", "Ready Screen").Config()
	wantPath := filepath.Join("testdata", "screen__testready", "fixture", "ready_screen.txt")
	if cfg.Path != wantPath {
		t.Fatalf("BuilderFor path=%q want %q", cfg.Path, wantPath)
	}
	if cfg.MinCols != DefaultMinCols || cfg.MinRows != DefaultMinRows {
		t.Fatalf("BuilderFor viewport=(%d,%d) want (%d,%d)", cfg.MinCols, cfg.MinRows, DefaultMinCols, DefaultMinRows)
	}
}

func TestBuilder_ConfigChain(t *testing.T) {
	normalize := func(s string) string { return strings.TrimSpace(s) }
	cfg := BuilderFor("screen", "default").
		Fixture("custom/path.txt").
		Normalize(normalize).
		Viewport(120, 40).
		Config()
	if cfg.Path != "custom/path.txt" {
		t.Fatalf("Fixture path=%q want custom/path.txt", cfg.Path)
	}
	if cfg.Normalize == nil {
		t.Fatal("Normalize did not set normalizer")
	}
	if cfg.MinCols != 120 || cfg.MinRows != 40 {
		t.Fatalf("Viewport=(%d,%d) want (120,40)", cfg.MinCols, cfg.MinRows)
	}
}
