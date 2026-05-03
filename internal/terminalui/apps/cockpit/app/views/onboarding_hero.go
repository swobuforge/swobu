package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
)

// FirstRunHero renders first-run setup framing lines.
func FirstRunHero(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	children := []view.ViewSpec[state.Model]{
		headerTextLine("set up your first workspace"),
	}
	if strings.TrimSpace(selectors.CreateDraftName(model)) == "" {
		children = append(children, headerTextLine("create one stable endpoint for your clients and choose where it runs"))
	} else {
		// Keep hero block height stable while name edits stream to avoid focus drift.
		children = append(children, StaticTextLine[state.Model](""))
	}
	return view.VStack(ctx, children...)
}

func headerTextLine(text string) view.ViewSpec[state.Model] {
	return IndentLeft[state.Model](StaticTextLine[state.Model](text), InsetSection)
}

// EmptyLine renders one blank spacer line.
func EmptyLine() view.ViewSpec[state.Model] {
	return StaticTextLine[state.Model]("")
}
