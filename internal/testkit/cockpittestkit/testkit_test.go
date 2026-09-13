package testkit

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/grindlemire/go-tui"

	"github.com/swobuforge/swobu/internal/testkit/testscreen"
	"github.com/swobuforge/swobu/internal/testkit/testscreen/fixture"
)

// TestRenderString_RendersSimpleText proves element.Render + Buffer.String produces
// deterministic text output against pinned go-tui.
func TestRenderString_RendersSimpleText(t *testing.T) {
	el := tui.New(tui.WithText("hello"))
	got := RenderString(el, 10, 3)
	want := "hello     \n          \n          "
	if got != want {
		t.Fatalf("RenderString() = %q, want %q", got, want)
	}
}

// TestRender_FlexLayout proves the flex layout engine is deterministic.
func TestRender_FlexLayout(t *testing.T) {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex),
		tui.WithDirection(tui.Column),
	)
	header := tui.New(tui.WithText("Cockpit"))
	hrule := tui.New(tui.WithHR())
	footer := tui.New(tui.WithText("quit: q"))
	root.AddChild(header, hrule, footer)

	got := RenderTrimmed(root, 20, 5)
	wantLines := []string{
		"Cockpit",
		"────────────────────",
		"quit: q",
		"",
		"",
	}
	want := strings.Join(wantLines, "\n")
	if got != want {
		t.Fatalf("RenderTrimmed() diff\ngot:\n%s\n\nwant:\n%s", got, want)
	}
}

// TestRenderTrimmed_StripsTrailingSpaces proves trailing-space normalization.
func TestRenderTrimmed_StripsTrailingSpaces(t *testing.T) {
	el := tui.New(tui.WithText("x"))
	got := RenderTrimmed(el, 8, 2)
	wantLines := []string{"x", ""}
	want := strings.Join(wantLines, "\n")
	if got != want {
		t.Fatalf("RenderTrimmed() = %q, want %q", got, want)
	}
}

// TestAssertNow_TextExists proves AssertNow works with our re-exported API.
func TestAssertNow_TextExists(t *testing.T) {
	root := tui.New(tui.WithText("target phrase"))
	rendered := RenderTrimmed(root, 20, 1)
	AssertNow(t, rendered, Text("target phrase").Exists())
}

// TestAssertVisual_DerivesCanonicalPath proves Cockpit visual fixtures use the
// same path shape as PTY visual assertions.
func TestAssertVisual_DerivesCanonicalPath(t *testing.T) {
	assertion := AssertVisual("default")
	cfg := assertion.fixture.Config()
	want := "testdata/testkit__testassertvisual_derivescanonicalpath/fixture/default.ansi"
	if cfg.Path != want {
		t.Fatalf("AssertVisual path = %q, want %q", cfg.Path, want)
	}
	if strings.Contains(cfg.Path, "_l") {
		t.Fatalf("visual fixture path must not include line-number entropy: %s", cfg.Path)
	}
}

// TestAssertVisual_ConfiguresSharedFixtureShape proves Cockpit and PTY visual
// assertions expose the same core configuration chain.
func TestAssertVisual_ConfiguresSharedFixtureShape(t *testing.T) {
	assertion := AssertVisual("ignored").Fixture("custom/path.ansi").Viewport(120, 40)
	cfg := assertion.fixture.Config()

	if cfg.Path != "custom/path.ansi" {
		t.Fatalf("Fixture() path = %q, want custom/path.ansi", cfg.Path)
	}
	if cfg.MinCols != 120 || cfg.MinRows != 40 {
		t.Fatalf("Viewport() = (%d, %d), want (120, 40)", cfg.MinCols, cfg.MinRows)
	}
}

func TestAssertVisual_ConfiguresExactViewport(t *testing.T) {
	cfg := AssertVisual("full").ExactViewport(100, 24).fixture.Config()
	if !cfg.ExactViewport || cfg.MinCols != 100 || cfg.MinRows != 24 {
		t.Fatalf("ExactViewport() config = %+v", cfg)
	}
}

// TestAssertVisual_CompareMissingFixture reports missing golden file through
// the shared fixture kernel and update environment.
func TestAssertVisual_CompareMissingFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.ansi")
	screen, err := testscreen.ParseANSI([]byte("snapshot text"))
	if err != nil {
		t.Fatal(err)
	}
	report := AssertVisual("missing").Fixture(path).Compare(screen)
	if report.Err == nil {
		t.Fatal("expected error for missing fixture")
	}
	if !strings.Contains(report.Err.Error(), "missing visual fixture") {
		t.Fatalf("expected 'missing visual fixture' error, got: %v", report.Err)
	}
	if !strings.Contains(report.Err.Error(), fixture.UpdateEnv+"=") {
		t.Fatalf("expected shared fixture update env in error, got: %v", report.Err)
	}
}

// TestReexports_AreKernelSymbols ensures we re-export actual kernel symbols,
// not duplicates (cargo test for testscreen family Rule 1).
func TestReexports_AreKernelSymbols(t *testing.T) {
	// If these compile, they are aliases to the kernel types declared via
	// type aliases in this package.
	var _ Predicate = Text("x").Exists()
	var _ Predicate = All(Text("x").Exists(), TextRE("y").Exists())
	var _ Predicate = Not(Text("x").Exists())

	// EvalNow must exist.
	_ = EvalNow
}
