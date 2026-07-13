package corelower

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

// TestLowerDevModeRendersDiagnostics proves that Lower in dev mode turns an
// invalid node tree into visible diagnostic text (red error nodes) instead of
// returning nil.
func TestLowerDevModeRendersDiagnostics(t *testing.T) {
	t.Parallel()

	// Duplicate keys trigger a validation error.
	node := core.Box[struct{}](
		core.Text[struct{}]("a").Key(core.K("dup")),
		core.Text[struct{}]("b").Key(core.K("dup")),
	)

	renderNode, err := Lower(node, EnvConfig{DevMode: true}, testCaster)
	if err != nil {
		t.Fatalf("lower with DevMode: unexpected error %v", err)
	}
	if renderNode == nil {
		t.Fatal("expected a diagnostic render node, got nil")
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 80, H: 4})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 4})
	paintNode(tree, buf, &layout.PaintContext{})
	got := buf.String()

	if !strings.Contains(got, "duplicate sibling key") {
		t.Fatalf("diagnostic text missing 'duplicate sibling key'; got:\n%s", got)
	}
}

// TestLowerDevModeSkipsValidationForValidTree proves that DevMode does not
// break lowering when the tree is valid.
func TestLowerDevModeSkipsValidationForValidTree(t *testing.T) {
	t.Parallel()

	node := core.Stack[struct{}](core.AxisVertical,
		core.Text[struct{}]("a").Key(core.K("a")),
		core.Text[struct{}]("b").Key(core.K("b")),
	)

	renderNode, err := Lower(node, EnvConfig{DevMode: true}, testCaster)
	if err != nil {
		t.Fatalf("lower valid tree with DevMode: %v", err)
	}
	if renderNode == nil {
		t.Fatal("expected render node")
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 8, H: 4})
	buf := paint.NewBuffer(geom.Rect{W: 8, H: 4})
	paintNode(tree, buf, &layout.PaintContext{})
	if got := buf.String(); got != "a\nb" {
		t.Fatalf("paint = %q, want a\\nb", got)
	}
}

// TestLowerProductionModeSkipsValidation proves that production mode (DevMode
// false) does NOT validate and returns the lowered tree even for invalid
// input.
func TestLowerProductionModeSkipsValidation(t *testing.T) {
	t.Parallel()

	node := core.Box[struct{}](
		core.Text[struct{}]("a").Key(core.K("dup")),
		core.Text[struct{}]("b").Key(core.K("dup")),
	)

	renderNode, err := Lower(node, EnvConfig{DevMode: false}, testCaster)
	if err != nil {
		t.Fatalf("lower with DevMode=false: unexpected error %v", err)
	}
	if renderNode == nil {
		t.Fatal("expected lowered render node in production mode")
	}
}

// TestLowerAssertRemainsStrict proves that LowerAssert still rejects invalid
// inputs and returns an error (test-only path).
func TestLowerAssertRemainsStrict(t *testing.T) {
	t.Parallel()

	node := core.Box[struct{}](
		core.Text[struct{}]("a").Key(core.K("dup")),
		core.Text[struct{}]("b").Key(core.K("dup")),
	)

	_, err := LowerAssert(node, EnvConfig{DevMode: true}, testCaster)
	if err == nil {
		t.Fatal("expected LowerAssert to reject invalid node")
	}
	if !strings.Contains(err.Error(), "duplicate sibling key") {
		t.Fatalf("error = %q, want duplicate sibling key", err.Error())
	}
}

// TestDiagnosticNodePaintsWithDangerStyle proves the diagnostic node renders
// red text via the style resolver.
func TestDiagnosticNodePaintsWithDangerStyle(t *testing.T) {
	t.Parallel()

	diags := []core.Diagnostic{
		{Severity: core.DiagnosticError, Path: "root", Message: "err1"},
		{Severity: core.DiagnosticError, Path: "root/0", Message: "err2"},
	}
	node := core.NewDiagnosticNode[struct{}](diags)

	renderNode, err := Lower(node, EnvConfig{Resolver: &StyleResolver{Palette: DefaultPalette}}, testCaster)
	if err != nil {
		t.Fatalf("lower diagnostic node: %v", err)
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 80, H: 4})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 4})
	paintNode(tree, buf, &layout.PaintContext{})
	got := buf.String()

	if !strings.Contains(got, "err1") || !strings.Contains(got, "err2") {
		t.Fatalf("expected diagnostic text 'err1' and 'err2'; got:\n%s", got)
	}
}
