// First-run explicit commit section.
package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildCreateSection renders the explicit first-run commit row.
func BuildCreateSection(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	if model.CurrentEndpoint != "" {
		return nil
	}
	var row retained.ViewSpec[state.Model]
	// Use a key-value row so blocked state is truthful and non-focusable when prerequisites are missing.
	if model.InteractionMode == state.InteractionModeBusySave {
		row = toolkitviews.NewKeyValueActionRow[state.Model]("create workspace", "creating...", "", nil)
	} else if len(createWorkspaceActions(model)) == 0 {
		row = RowKVWithHooks("create workspace", createWorkspaceStatus(model), "", nil, nil, nil)
	} else {
		row = RowActionWithHooks("create workspace", createWorkspaceStatus(model), "create", func() []update.Action {
			return createWorkspaceActions(model)
		}, nil, focusAffordance("create", false))
	}
	return Section(SectionCreate, row)
}
