package corelower

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

// TestLowerActionIntentRoutingSpace proves the intent map is read from
// InteractionSpec.Keymap — not hardcoded to Enter. A Space key binding
// triggers the signal; Enter with a different binding does not.
func TestLowerActionIntentRoutingSpace(t *testing.T) {
	t.Parallel()

	renderNode, err := Lower(
		core.Action("action", core.SignalEvent[struct{}]{Kind: "opened"}).
			Interaction(core.InteractionSpec[struct{}]{
				Focus: core.FocusSpec{Mode: core.Focusable},
				Keymap: []core.KeyBindingSpec{
					{Pattern: core.KeyMatch{Name: "space"}, Intent: core.IntentActivate},
				},
			}),
		EnvConfig{},
		testCaster,
	)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 10, H: 1})
	handler := tree.RenderNode.(interaction.EventHandler)

	// Enter should not trigger when only Space is mapped.
	enterActions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, tree)
	if len(enterActions) != 0 {
		t.Fatalf("enter actions = %d, want 0", len(enterActions))
	}

	// Space should trigger.
	spaceActions := handler.HandleEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeySpace}, tree)
	if len(spaceActions) != 1 {
		t.Fatalf("space actions = %d, want 1", len(spaceActions))
	}
}

// TestLowerActionStylePaint proves that lowerAction stores the resolved
// paint.Style and Paint applies it via WithStyle.
func TestLowerActionStylePaint(t *testing.T) {
	t.Parallel()

	renderNode, err := Lower(
		core.Action("styled", core.SignalEvent[struct{}]{Kind: "k"}).
			Style(core.Style{Token: core.TokenTextDanger}),
		EnvConfig{},
		testCaster,
	)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 10, H: 1})
	buf := paint.NewBuffer(geom.Rect{W: 10, H: 1})
	paintNode(tree, buf, &layout.PaintContext{})

	// Verify the text is rendered.
	if got := buf.String(); got != "  styled" {
		t.Fatalf("text = %q, want '  styled'", got)
	}

	// Verify the cell has the resolved style (red fg from danger token).
	cell := buf.Cell(2, 0) // column 2 is the 's' in "  styled"
	if cell.Style.Fg != paint.ColorRed {
		t.Fatalf("style fg = %v, want red", cell.Style.Fg)
	}
}

// TestLowerBoxRespectsCoreLayoutSize proves that core.Layout.Size values
// pass through into the retained Sizing without override.
func TestLowerBoxRespectsCoreLayoutSize(t *testing.T) {
	t.Parallel()

	renderNode, err := Lower(
		core.Box[struct{}](
			core.Text[struct{}]("hi"),
		).Layout(core.Layout{
			Size: core.Size{
				Width:  core.Fixed(20),
				Height: core.Fixed(5),
			},
		}),
		EnvConfig{},
		testCaster,
	)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}

	box, ok := renderNode.(*layout.BoxRenderNode)
	if !ok {
		t.Fatalf("type = %T, want BoxRenderNode", renderNode)
	}

	// Width should be SizeFixed with Fixed.W = 20.
	if box.Sizing.W != layout.SizeFixed {
		t.Fatalf("width mode = %v, want SizeFixed", box.Sizing.W)
	}
	if box.Sizing.Fixed.W != 20 {
		t.Fatalf("width fixed = %d, want 20", box.Sizing.Fixed.W)
	}

	// Height should be SizeFixed with Fixed.H = 5.
	if box.Sizing.H != layout.SizeFixed {
		t.Fatalf("height mode = %v, want SizeFixed", box.Sizing.H)
	}
	if box.Sizing.Fixed.H != 5 {
		t.Fatalf("height fixed = %d, want 5", box.Sizing.Fixed.H)
	}
}

// TestLowerBoxFillLayoutSize proves Fill dimensions map to SizeGrow.
func TestLowerBoxFillLayoutSize(t *testing.T) {
	t.Parallel()

	renderNode, err := Lower(
		core.Box[struct{}](core.Text[struct{}]("x")).
			Layout(core.Layout{
				Size: core.Size{
					Width:  core.Fill(1),
					Height: core.Fill(2),
				},
			}),
		EnvConfig{},
		testCaster,
	)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}

	box := renderNode.(*layout.BoxRenderNode)
	if box.Sizing.W != layout.SizeGrow {
		t.Fatalf("width mode = %v, want SizeGrow", box.Sizing.W)
	}
	if box.Sizing.H != layout.SizeGrow {
		t.Fatalf("height mode = %v, want SizeGrow", box.Sizing.H)
	}
}

// TestLowerBoxMinMaxLayoutSize proves MinMax dimensions map correctly.
func TestLowerBoxMinMaxLayoutSize(t *testing.T) {
	t.Parallel()

	renderNode, err := Lower(
		core.Box[struct{}](core.Text[struct{}]("x")).
			Layout(core.Layout{
				Size: core.Size{
					Width: core.MinMax(5, core.Fixed(100)),
				},
			}),
		EnvConfig{},
		testCaster,
	)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}

	box := renderNode.(*layout.BoxRenderNode)
	if box.Sizing.Min.W != 5 {
		t.Fatalf("min width = %d, want 5", box.Sizing.Min.W)
	}
	if box.Sizing.Max.W != 100 {
		t.Fatalf("max width = %d, want 100", box.Sizing.Max.W)
	}
}
