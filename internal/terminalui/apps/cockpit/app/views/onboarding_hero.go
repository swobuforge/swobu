package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// FirstRunHero renders first-run setup framing lines.
func FirstRunHero(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	children := []retained.ViewSpec[state.Model]{
		headerTextLine("unbundle your ai stack"),
	}
	if strings.TrimSpace(selectors.CreateDraftName(model)) == "" {
		children = append(children, headerTextLine("set one local boundary between your client and your model backend"))
	} else {
		// Keep hero block height stable while name edits stream to avoid focus drift.
		children = append(children, StaticTextLine[state.Model](""))
	}
	return retained.VStack(ctx, children...)
}

func headerTextLine(text string) retained.ViewSpec[state.Model] {
	return IndentLeft[state.Model](StaticTextLine[state.Model](text), InsetSection)
}

// EmptyLine renders one blank spacer line.
func EmptyLine() retained.ViewSpec[state.Model] {
	return StaticTextLine[state.Model]("")
}
