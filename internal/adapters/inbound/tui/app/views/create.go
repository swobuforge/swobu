// First-run explicit commit section.
package views

import (
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
	toolkitviews "github.com/metrofun/swobu/internal/adapters/inbound/tui/toolkit/views"
)

// BuildCreateSection renders the explicit first-run commit row.
func BuildCreateSection(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	if model.CurrentEndpoint != "" {
		return nil
	}
	var row view.ViewSpec[state.Model]
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
