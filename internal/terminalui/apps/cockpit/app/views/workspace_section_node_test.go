package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestBuildWorkspaceSectionNode_ReturnsCoreNode(t *testing.T) {
	t.Parallel()

	model := state.Model{
		HeaderStatus:       "ready",
		Endpoints:          []string{"acme"},
		CurrentEndpoint:    "acme",
		WorkspaceSaveError: "workspace name already exists",
		WorkspaceCopyNote:  "copied endpoint",
	}

	node := BuildWorkspaceSectionNode(model)
	if diags := core.Validate(node); len(diags) > 0 {
		t.Fatalf("validation failed: %v", diags)
	}

	renderNode, err := corelower.Lower(node, corelower.EnvConfig{}, func(a state.Action) update.Action {
		return a
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if renderNode == nil {
		t.Fatal("expected render node")
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 96, H: 24})
	buf := paint.NewBuffer(geom.Rect{W: 96, H: 24})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := strings.TrimSpace(buf.String())

	for _, want := range []string{
		"workspace",
		"name",
		"acme",
		"edit",
		"endpoint",
		"copy",
		"delete workspace",
		"workspace name already exists",
		"copied endpoint",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render = %q, want %q", out, want)
		}
	}
}

func TestBuildWorkspaceSectionNode_CreateLaneBusySaveReturnsCoreNode(t *testing.T) {
	t.Parallel()

	model := state.Model{
		InteractionMode: state.InteractionModeBusySave,
	}

	node := BuildWorkspaceSectionNode(model)
	if diags := core.Validate(node); len(diags) > 0 {
		t.Fatalf("validation failed: %v", diags)
	}

	renderNode, err := corelower.Lower(node, corelower.EnvConfig{}, func(a state.Action) update.Action {
		return a
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if renderNode == nil {
		t.Fatal("expected render node")
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 96, H: 24})
	buf := paint.NewBuffer(geom.Rect{W: 96, H: 24})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := strings.TrimSpace(buf.String())

	for _, want := range []string{
		"workspace",
		"name",
		"choose a workspace name",
		"endpoint",
		"<slug>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render = %q, want %q", out, want)
		}
	}
}
