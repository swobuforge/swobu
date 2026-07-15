package cockpit

import (
	tui "github.com/grindlemire/go-tui"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	help_surface "github.com/swobuforge/swobu/internal/cockpit/surfaces/help"
	workspace_plane "github.com/swobuforge/swobu/internal/cockpit/surfaces/workspace_plane"
)

// Cockpit composes the operator shell, active surface, and static global frame.
//
// It owns shell composition and selected top-level readmodel data. It does not
// own feature drafts, submit lifecycle, route mutation, target mutation, or run
// execution.
type Cockpit struct {
	Model          readmodel.CockpitReadModel
	WorkspacePlane *workspace_plane.ViewModel
	HelpSurface    *help_surface.ViewView
}

// NewCockpit constructs the root shell from an already-loaded readmodel.
func NewCockpit(model readmodel.CockpitReadModel) *Cockpit {
	return &Cockpit{
		Model:          model,
		WorkspacePlane: workspace_plane.View(model.SelectedWorkspace),
		HelpSurface:    help_surface.View(model.Help),
	}
}

templ (c *Cockpit) Render() {
	<div class="flex-col h-full w-full">
		@ShellHeader(c.Model)
		<hr />
		if c.Model.Surface == readmodel.CockpitHelpSurface {
			@c.HelpSurface
		} else {
			@c.WorkspacePlane
		}
		<hr />
		@ShellFooter(c.Model)
	</div>
}

templ ShellHeader(model readmodel.CockpitReadModel) {
	<div class="flex-row w-full">
		<span class="w-9 font-bold">SWOBU</span>
		<div class="flex-row gap-1 grow">
			for _, tab := range model.Tabs {
				if tab.Selected {
					<span>{activeTabLabel(tab)}</span>
				} else {
					<span>{inactiveTabLabel(tab)}</span>
				}
			}
		</div>
		<span>{model.EnvironmentLabel}</span>
	</div>
}

templ ShellFooter(model readmodel.CockpitReadModel) {
	if model.Surface == readmodel.CockpitHelpSurface {
		<div class="flex-row gap-3">
			<span>↑↓ move</span>
			<span>↵ open/copy</span>
			<span>Tab next</span>
			<span>Shift+Tab prev</span>
			<span>esc back</span>
		</div>
	} else if model.SelectedWorkspace.IsDraft() {
		<div class="flex-row gap-3">
			<span>Tab next</span>
			<span>Shift+Tab prev</span>
			<span>esc back</span>
		</div>
	} else {
		<div class="flex-row gap-3">
			<span>↑↓ move</span>
			<span>↵ open</span>
			<span>? help</span>
			<span>esc back</span>
		</div>
	}
}

func activeTabLabel(tab readmodel.WorkspaceTabReadModel) string {
	return "[› " + tabLabel(tab) + "]"
}

func inactiveTabLabel(tab readmodel.WorkspaceTabReadModel) string {
	return "[" + tabLabel(tab) + "]"
}

func tabLabel(tab readmodel.WorkspaceTabReadModel) string {
	switch tab.Kind {
	case readmodel.WorkspaceTabDraft:
		return "+"
	case readmodel.WorkspaceTabHelp:
		return "?"
	default:
		return tab.Slug
	}
}
