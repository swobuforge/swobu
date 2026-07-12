package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/reconcile"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func TestBuildWorkspaceSection_RendersCoreBridgeEndpointRow(t *testing.T) {
	t.Parallel()

	model := state.Model{
		CurrentEndpoint: "acme",
		Endpoints:       []string{"acme"},
		HeaderStatus:    "saved",
	}
	ctx := &retained.Context[state.Model]{
		Local: reconcile.NewLocalStore().Scope(1),
		Model: func() state.Model { return model },
	}
	spec := BuildWorkspaceSection(ctx)
	node := retained.Materialize(ctx, spec)
	tree := (&layout.TreeBuilder{}).Build(node, geom.Rect{W: 100, H: 10})
	buf := paint.NewBuffer(geom.Rect{W: 100, H: 10})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := strings.TrimSpace(buf.String()) // swobu:io-string source=domain
	if !strings.Contains(out, "copy ↵") {
		t.Fatalf("render = %q, want core-backed copy action", out)
	}
	if !strings.Contains(out, "edit ↵") {
		t.Fatalf("render = %q, want saved-name edit hint", out)
	}
	if !strings.Contains(out, "workspace") {
		t.Fatalf("render = %q, want workspace section", out)
	}
}

func TestWorkspaceDeleteRowEmitsFocusAffordanceAndDeleteAction(t *testing.T) {
	t.Parallel()

	t.Run("enabled", func(t *testing.T) {
		t.Parallel()

		tree := renderRowSpec(t, workspaceDeleteRow("acme"))
		focusEvents, ok := tree.RenderNode.(interaction.FocusEvents)
		if !ok {
			t.Fatalf("type = %T, want interaction.FocusEvents", tree.RenderNode)
		}
		focusActions := focusEvents.OnFocus(tree)
		if len(focusActions) != 1 {
			t.Fatalf("focus action count = %d, want 1", len(focusActions))
		}
		focusSignal, ok := focusActions[0].(update.CoreSignalAction)
		if !ok {
			t.Fatalf("focus action = %T, want update.CoreSignalAction", focusActions[0])
		}
		payload, ok := focusSignal.Signal.Data.(state.SetFocusedRowAffordance)
		if !ok {
			t.Fatalf("focus payload type = %T, want state.SetFocusedRowAffordance", focusSignal.Signal.Data)
		}
		if payload.Verb != "delete" || payload.AllowSpace {
			t.Fatalf("focus payload = %#v, want verb=delete allowSpace=false", payload)
		}

		handler, ok := tree.RenderNode.(interaction.EventHandler)
		if !ok {
			t.Fatalf("type = %T, want interaction.EventHandler", tree.RenderNode)
		}
		actions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, tree)
		if len(actions) != 1 {
			t.Fatalf("enter action count = %d, want 1", len(actions))
		}
		signal, ok := actions[0].(update.CoreSignalAction)
		if !ok {
			t.Fatalf("enter action = %T, want update.CoreSignalAction", actions[0])
		}
		requested, ok := signal.Signal.Data.(state.WorkspaceDeleteRequested)
		if !ok {
			t.Fatalf("enter payload type = %T, want state.WorkspaceDeleteRequested", signal.Signal.Data)
		}
		if requested.Name != "acme" {
			t.Fatalf("enter payload name = %q, want acme", requested.Name)
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()

		tree := renderRowSpec(t, workspaceDeleteRow(""))
		focusEvents, ok := tree.RenderNode.(interaction.FocusEvents)
		if !ok {
			t.Fatalf("type = %T, want interaction.FocusEvents", tree.RenderNode)
		}
		focusActions := focusEvents.OnFocus(tree)
		if len(focusActions) != 1 {
			t.Fatalf("focus action count = %d, want 1", len(focusActions))
		}
		focusSignal, ok := focusActions[0].(update.CoreSignalAction)
		if !ok {
			t.Fatalf("focus action = %T, want update.CoreSignalAction", focusActions[0])
		}
		payload, ok := focusSignal.Signal.Data.(state.SetFocusedRowAffordance)
		if !ok {
			t.Fatalf("focus payload type = %T, want state.SetFocusedRowAffordance", focusSignal.Signal.Data)
		}
		if payload.Verb != "delete" || payload.AllowSpace {
			t.Fatalf("focus payload = %#v, want verb=delete allowSpace=false", payload)
		}

		handler, ok := tree.RenderNode.(interaction.EventHandler)
		if !ok {
			t.Fatalf("type = %T, want interaction.EventHandler", tree.RenderNode)
		}
		actions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, tree)
		if len(actions) != 0 {
			t.Fatalf("enter action count = %d, want 0", len(actions))
		}
	})
}
