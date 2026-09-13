package fixture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/testkit/testscreen"
)

func TestPath_BuildsCanonicalVisualFixturePath(t *testing.T) {
	got := Path("default_launch__testdefaultlaunch", "Ready Screen")
	want := filepath.Join("testdata", "default_launch__testdefaultlaunch", "fixture", "ready_screen.ansi")
	if got != want {
		t.Fatalf("Path()=%q want %q", got, want)
	}
}

func TestPath_EmptyAssertionUsesDefault(t *testing.T) {
	got := Path("cockpit__testrenderdefaultworkspace", "")
	want := filepath.Join("testdata", "cockpit__testrenderdefaultworkspace", "fixture", "default.ansi")
	if got != want {
		t.Fatalf("Path()=%q want %q", got, want)
	}
}

func TestCompareScreen_UsesSharedUpdateEnv(t *testing.T) {
	t.Setenv(UpdateEnv, "1")
	cfg := BuilderFor("screen", "default").Fixture(PathIn(t.TempDir(), "screen", "default")).Config()
	report := CompareScreen(screenFromText(t, "fresh"), cfg)
	if report.Err != nil {
		t.Fatalf("CompareScreen() unexpected error: %v", report.Err)
	}
	if !strings.HasSuffix(report.FixturePath, filepath.Join("screen", "fixture", "default.ansi")) {
		t.Fatalf("FixturePath=%q", report.FixturePath)
	}
}

func TestCompareScreen_RejectsCasualPromotionValues(t *testing.T) {
	for _, bad := range []string{"true", "yes", "on", "please", "i-have-reviewed"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv(UpdateEnv, bad)
			cfg := BuilderFor("screen", "default").Fixture(PathIn(t.TempDir(), "screen", "default")).Config()
			report := CompareScreen(screenFromText(t, "fresh"), cfg)
			if report.Err == nil {
				t.Fatal("expected error for casual promotion value, got nil")
			}
			if !strings.Contains(report.Err.Error(), "must be unset or 1") {
				t.Fatalf("error should explain update values, got: %v", report.Err)
			}
		})
	}
}

func screenFromText(t testing.TB, text string) testscreen.Screen {
	t.Helper()
	screen, err := testscreen.ParseANSI([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return screen
}

func TestBuilderFor_UsesSharedVisualDefaults(t *testing.T) {
	cfg := BuilderFor("screen__testready", "Ready Screen").Config()
	wantPath := filepath.Join("testdata", "screen__testready", "fixture", "ready_screen.ansi")
	if cfg.Path != wantPath {
		t.Fatalf("BuilderFor path=%q want %q", cfg.Path, wantPath)
	}
	if cfg.MinCols != DefaultMinCols || cfg.MinRows != DefaultMinRows {
		t.Fatalf("BuilderFor viewport=(%d,%d) want (%d,%d)", cfg.MinCols, cfg.MinRows, DefaultMinCols, DefaultMinRows)
	}
}

func TestBuilder_ConfigChain(t *testing.T) {
	cfg := BuilderFor("screen", "default").
		Fixture("custom/path.ansi").
		Viewport(120, 40).
		Config()
	if cfg.Path != "custom/path.ansi" {
		t.Fatalf("Fixture path=%q want custom/path.ansi", cfg.Path)
	}
	if cfg.MinCols != 120 || cfg.MinRows != 40 {
		t.Fatalf("Viewport=(%d,%d) want (120,40)", cfg.MinCols, cfg.MinRows)
	}
}

func TestBuilder_ExactViewportIsDistinctFromMinimumViewport(t *testing.T) {
	cfg := BuilderFor("screen", "full").ExactViewport(100, 24).Config()
	if !cfg.ExactViewport || cfg.MinCols != 100 || cfg.MinRows != 24 {
		t.Fatalf("ExactViewport config = %+v", cfg)
	}
}

func TestCompareScreen_ExactPromotionRejectsWrongActualGeometry(t *testing.T) {
	t.Setenv(UpdateEnv, "1")
	path := filepath.Join(t.TempDir(), "screen.ansi")
	cfg := BuilderFor("screen", "full").Fixture(path).ExactViewport(100, 36).Config()
	report := CompareScreen(testscreen.New(120, 40), cfg)
	if report.Err == nil || !strings.Contains(report.Err.Error(), "actual viewport is 120x40, want exact 100x36") {
		t.Fatalf("exact promotion report = %#v", report)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid exact fixture was written: %v", err)
	}
}
