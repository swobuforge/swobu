package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// TestBuildHelpSectionNode_ReturnsCoreNode proves the canonical path returns a
// valid core.Node[state.Action] that can be lowered without errors.
func TestBuildHelpSectionNode_ReturnsCoreNode(t *testing.T) {
	t.Parallel()

	model := state.Model{}
	node := BuildHelpSectionNode(model)

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

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 80, H: 8})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 8})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "ask question") {
		t.Fatalf("render = %q, want ask question", out)
	}
	if !strings.Contains(out, "file issue") {
		t.Fatalf("render = %q, want file issue", out)
	}
}

// TestBuildHelpSectionNode_EmitsCorrectSignal proves the action row emits the
// right typed signal on enter.
func TestBuildHelpSectionNode_EmitsCorrectSignal(t *testing.T) {
	t.Parallel()

	model := state.Model{}
	node := BuildHelpSectionNode(model)

	renderNode, err := corelower.Lower(node, corelower.EnvConfig{}, func(a state.Action) update.Action {
		return a
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 80, H: 8})
	// Find first focusable action node.
	var actionHandler interaction.EventHandler
	walkLayoutTree(tree, func(n *layout.LayoutNode) bool {
		if h, ok := n.RenderNode.(interaction.EventHandler); ok {
			actionHandler = h
			return false // stop walking
		}
		return true
	})
	if actionHandler == nil {
		t.Fatal("expected focusable action in help tree")
	}

	actions := actionHandler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, tree)
	if len(actions) != 1 {
		t.Fatalf("action count = %d, want 1", len(actions))
	}
	typedAction, ok := actions[0].(state.Action)
	if !ok {
		t.Fatalf("action = %T, want state.Action", actions[0])
	}
	req, ok := typedAction.(state.OpenSupportLinkRequested)
	if !ok {
		t.Fatalf("action = %T, want OpenSupportLinkRequested", typedAction)
	}
	if req.Label != "ask question" {
		t.Fatalf("label = %q, want ask question", req.Label)
	}
}

// TestBuildHelpSectionNode_WithFallbackURL proves the fallback URL appears as
// a second text line when the note matches.
func TestBuildHelpSectionNode_WithFallbackURL(t *testing.T) {
	t.Parallel()

	model := state.Model{
		HelpNote: "ask question open failed; fallback https://backup.example.com",
	}
	node := BuildHelpSectionNode(model)

	renderNode, err := corelower.Lower(node, corelower.EnvConfig{}, func(a state.Action) update.Action {
		return a
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 80, H: 8})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 8})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "backup.example.com") {
		t.Fatalf("expected fallback URL in render, got:\n%s", out)
	}
}

func walkLayoutTree(n *layout.LayoutNode, fn func(*layout.LayoutNode) bool) bool {
	if !fn(n) {
		return false
	}
	for _, child := range n.Kids {
		if !walkLayoutTree(child, fn) {
			return false
		}
	}
	return true
}
