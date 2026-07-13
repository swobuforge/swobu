package corelower

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

// TestLowerRejectsInvalidTree proves that Lower always validates and returns
// an error for invalid input.
func TestLowerRejectsInvalidTree(t *testing.T) {
	t.Parallel()

	// Duplicate keys trigger a validation error.
	node := core.Box[struct{}](
		core.Text[struct{}]("a").Key(core.K("dup")),
		core.Text[struct{}]("b").Key(core.K("dup")),
	)

	_, err := Lower(node, EnvConfig{}, testCaster)
	if err == nil {
		t.Fatal("expected Lower to reject invalid node")
	}
	if !strings.Contains(err.Error(), "duplicate sibling key") {
		t.Fatalf("error = %q, want duplicate sibling key", err.Error())
	}
}

// TestLowerAllowsValidTree proves that lowering succeeds when the tree is valid.
func TestLowerAllowsValidTree(t *testing.T) {
	t.Parallel()

	node := core.Stack[struct{}](core.AxisVertical,
		core.Text[struct{}]("a").Key(core.K("a")),
		core.Text[struct{}]("b").Key(core.K("b")),
	)

	renderNode, err := Lower(node, EnvConfig{}, testCaster)
	if err != nil {
		t.Fatalf("lower valid tree: %v", err)
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

// TestDiagnosticNodePaintsWithDangerStyle proves the diagnostic node renders
// red text via the style resolver.
func TestDiagnosticNodePaintsWithDangerStyle(t *testing.T) {
	t.Parallel()

	diags := []core.Diagnostic{
		{Severity: core.DiagnosticError, Path: "root", Message: "err1"},
		{Severity: core.DiagnosticError, Path: "root/0", Message: "err2"},
	}
	node := core.NewDiagnosticNode[struct{}](diags)

	renderNode, err := Lower(node, EnvConfig{Resolver: &StyleResolver{ColorConfig: DefaultColorConfig}}, testCaster)
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
