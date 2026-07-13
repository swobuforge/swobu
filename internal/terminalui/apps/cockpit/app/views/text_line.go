package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// StaticTextLineNode returns a semantic core node for one non-focusable line.
func StaticTextLineNode(text string) core.Node[state.Action] {
	text = strings.TrimSpace(text) // swobu:io-string source=boundary
	return core.Text[state.Action](text)
}

// StaticTextLine renders one non-focusable line.
func StaticTextLine(text string) retained.ViewSpec[state.Model] {
	return CoreNodeAsRetained(StaticTextLineNode(text))
}

// IndentLeftNode composes a node under a parent-owned left inset.
func IndentLeftNode(cols int) func(core.Node[state.Action]) core.Node[state.Action] {
	return func(child core.Node[state.Action]) core.Node[state.Action] {
		if cols <= 0 {
			return child
		}
		return child.Layout(core.Layout{
			Size: core.Size{
				Width:  core.Fit(),
				Height: core.Fit(),
			},
			Inset: core.Insets{Left: cols},
		})
	}
}

// IndentLeft composes a view under a parent-owned left inset.
func IndentLeft[M any](child retained.ViewSpec[M], cols int) retained.ViewSpec[M] {
	if cols <= 0 {
		return child
	}
	return retained.Padded[M](child, 0, 0, 0, cols)
}
