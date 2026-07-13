package inspector

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
)

func TestRenderDiagnostics_NoErrors(t *testing.T) {
	root := core.Box[struct{}](core.Text[struct{}]("hello"))
	got := RenderDiagnostics(root)
	if got != "(no diagnostics)" {
		t.Fatalf("got %q, want no diagnostics", got)
	}
}

func TestRenderDiagnostics_WithErrors(t *testing.T) {
	root := core.Box[struct{}](
		core.Text[struct{}]("a").Key(core.K("dup")),
		core.Text[struct{}]("b").Key(core.K("dup")),
	)
	got := RenderDiagnostics(root)
	if !strings.Contains(got, "error") {
		t.Fatalf("expected error in output, got %q", got)
	}
	if !strings.Contains(got, "duplicate sibling key") {
		t.Fatalf("expected duplicate key message, got %q", got)
	}
}

func TestRenderLayout(t *testing.T) {
	root := core.Box[struct{}](
		core.Text[struct{}]("hello"),
	)
	got := RenderLayout(root)
	if !strings.Contains(got, "Box") {
		t.Fatalf("expected Box in output, got %q", got)
	}
	if !strings.Contains(got, "Text") {
		t.Fatalf("expected Text in output, got %q", got)
	}
}

func TestRenderFocus_Empty(t *testing.T) {
	root := core.Box[struct{}](core.Text[struct{}]("hello"))
	got := RenderFocus(root)
	if got != "(no focusable nodes)" {
		t.Fatalf("got %q, want no focusable nodes", got)
	}
}

func TestRenderFocus_WithFocusables(t *testing.T) {
	root := core.Box[struct{}](
		core.Text[struct{}]("a").Key(core.K("a")).Interaction(core.InteractionSpec[struct{}]{
			Focus: core.FocusSpec{Mode: core.Focusable, ID: core.FocusID("a")},
		}),
		core.Text[struct{}]("b").Key(core.K("b")).Interaction(core.InteractionSpec[struct{}]{
			Focus: core.FocusSpec{Mode: core.Focusable, ID: core.FocusID("b")},
		}),
	)
	got := RenderFocus(root)
	if !strings.Contains(got, "a") {
		t.Fatalf("expected 'a' in output, got %q", got)
	}
	if !strings.Contains(got, "b") {
		t.Fatalf("expected 'b' in output, got %q", got)
	}
}

func TestRender_UnknownMode(t *testing.T) {
	root := core.Text[struct{}]("x")
	got := Render(Mode("bogus"), root)
	if !strings.Contains(got, "unknown inspector mode") {
		t.Fatalf("expected unknown mode error, got %q", got)
	}
}

func TestRender_DiagnosticsMode(t *testing.T) {
	root := core.Text[struct{}]("ok")
	got := Render(ModeDiagnostics, root)
	if got != "(no diagnostics)" {
		t.Fatalf("got %q, want no diagnostics", got)
	}
}
