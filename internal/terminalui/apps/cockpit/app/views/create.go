package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/components/compound"
	"github.com/swobuforge/swobu/internal/terminalui/core"
)

// BuildCreateSectionNode returns the create section as a pure semantic core.Node.
// This is the canonical core-algebra path for the create section.
func BuildCreateSectionNode(model state.Model) core.Node[state.Action] {
	if model.CurrentEndpoint != "" {
		return core.Box[state.Action]()
	}

	var row core.Node[state.Action]
	switch {
	case model.InteractionMode == state.InteractionModeBusySave:
		row = settingStaticRowNode("create workspace", "creating...")
	case len(createWorkspaceActions(model)) == 0:
		row = settingStaticRowNode("create workspace", createWorkspaceStatus(model))
	default:
		parsedName, _ := validateWorkspaceName(selectors.CreateDraftName(model), model.Endpoints, "")
		row = SettingActionRowNode(
			core.K("create/workspace"),
			"create workspace",
			createWorkspaceStatus(model),
			"create",
			state.WorkspaceCreateRequested{Name: parsedName},
			false,
		)
	}

	return compound.SectionNode[state.Action]("create", row)
}
