package inspector

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/core"
)

// Overlay returns a new core.Node that renders the original tree plus an
// inspector overlay if the inspector is enabled.  When enabled, the overlay
// shows the inspector output in a side panel without mutating the app state.
// The overlay is stateless: it re-renders from the node tree on each call.
func Overlay[E any](mode Mode, tree core.Node[E]) core.Node[E] {
	if !Enabled() {
		return tree
	}
	output := Render(mode, tree)
	lines := strings.Split(output, "\n")
	// Build inspector panel as a column of text lines
	panelChildren := make([]core.Node[E], 0, len(lines)+1)
	panelChildren = append(panelChildren, core.Text[E]("=== INSPECTOR ==="))
	for _, line := range lines {
		if line != "" {
			panelChildren = append(panelChildren, core.Text[E](line))
		}
	}
	panel := core.Box[E](panelChildren...).
		Style(core.Style{Token: core.TokenSurfaceSelected}).
		Layout(core.Layout{
			Size: core.Size{Width: core.Fill(1), Height: core.Fit()},
		})

	// Layer the inspector panel on top of the original tree (at the bottom)
	return core.Layer[E](
		tree,
		panel.Layout(core.Layout{
			Size: core.Size{Width: core.Fill(1), Height: core.Fit()},
		}),
	)
}
