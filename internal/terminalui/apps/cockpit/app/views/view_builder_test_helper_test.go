package views

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appstate "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/reconcile"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// renderCockpitView materializes one retained ViewSpec into a string render.
// Needed by tests that still exercise retained view builders during the migration.
func renderCockpitView(t *testing.T, ctx *retained.Context[appstate.Model], spec retained.ViewSpec[appstate.Model]) string {
	t.Helper()
	model := ctx.Model()
	node, _, _, _ := reconcile.New[appstate.Model](reconcile.NewLocalStore()).Reconcile(
		spec,
		&model,
		func(update.Action) {},
		func(update.Action) {},
	)
	tree := (&layout.TreeBuilder{}).Build(node, geom.Rect{W: 80, H: 24})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 24})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	return strings.TrimSpace(buf.String())
}

// paintLayoutTree is defined in core_bridge_paint_test.go (same package).
// It renders a retained layout tree into a paint buffer.

// effectBridge is the test bridge from EffectOnce to update.Effect.
type effectBridge struct{ once appstate.EffectOnce }

func (b effectBridge) Execute(ctx context.Context) []update.Action {
	action := b.once.Run(ctx)
	if action == nil {
		return nil
	}
	return []update.Action{action}
}

// assertNoError is a test helper that fails on unexpected error values.
func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// pointerToModel creates a pointer copy for reconcile.New callers.
func pointerToModel(m appstate.Model) *appstate.Model {
	return &m
}

// stubModel is a minimal model for compilation-only tests.
func stubModel() appstate.Model {
	return appstate.Model{HeaderStatus: "ready"}
}

// renderCockpitViewString provides a package-level render for retained specs.
func renderCockpitViewString(spec retained.ViewSpec[appstate.Model], model appstate.Model) string {
	node, _, _, _ := reconcile.New[appstate.Model](reconcile.NewLocalStore()).Reconcile(
		spec,
		&model,
		func(update.Action) {},
		func(update.Action) {},
	)
	tree := (&layout.TreeBuilder{}).Build(node, geom.Rect{W: 80, H: 24})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 24})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	return strings.TrimSpace(buf.String())
}

// formatRenderResult wraps a render string in a debug-friendly envelope.
func formatRenderResult(label, out string) string {
	return fmt.Sprintf("%s:\n%s", label, out)
}
