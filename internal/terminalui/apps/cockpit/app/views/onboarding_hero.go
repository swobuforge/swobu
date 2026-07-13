package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
)

// FirstRunHero renders first-run setup framing lines.
func FirstRunHero(model state.Model) core.Node[state.Action] {
	children := []core.Node[state.Action]{
		headerTextLine("unbundle your ai stack"),
	}
	if strings.TrimSpace(selectors.CreateDraftName(model)) == "" { // swobu:io-string source=boundary
		children = append(children, headerTextLine("set one local boundary between your client and your model backend"))
	} else {
		// Keep hero block height stable while name edits stream to avoid focus drift.
		children = append(children, core.Text[state.Action](""))
	}
	return core.Stack[state.Action](core.AxisVertical, children...).
		Layout(core.Layout{
			Flow: core.Flow{Mode: core.FlowStack, Axis: core.AxisVertical},
		})
}

func headerTextLine(text string) core.Node[state.Action] {
	return core.Text[state.Action](text)
}

// EmptyLine renders one blank spacer line.
func EmptyLine() core.Node[state.Action] {
	return core.Text[state.Action]("")
}
