package cockpit

import (
	tui "github.com/grindlemire/go-tui"

	help_page "github.com/swobuforge/swobu/internal/cockpit/pages/help"
	workspace_page "github.com/swobuforge/swobu/internal/cockpit/pages/workspace"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// Cockpit composes the operator shell, active page, and static global frame.
//
// It owns shell composition and selected top-level readmodel data. It does not
// own feature drafts, submit lifecycle, route mutation, target mutation, or run
// execution.
type Cockpit struct {
	Model         readmodel.CockpitReadModel
	WorkspacePage *workspace_page.PageView
	HelpPage      *help_page.ViewView
}

// NewCockpit constructs the root shell from an already-loaded readmodel.
func NewCockpit(model readmodel.CockpitReadModel) *Cockpit {
	return &Cockpit{
		Model:         model,
		WorkspacePage: workspace_page.Page(model.SelectedWorkspace),
		HelpPage:      help_page.View(model.Help),
	}
}

templ (c *Cockpit) Render() {
	<div class="flex-col h-full w-full">
		@ShellHeader(c.Model)
		<hr />
		if c.Model.ActivePage == readmodel.CockpitHelpPage {
			@c.HelpPage
		} else {
			@c.WorkspacePage
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
	if model.ActivePage == readmodel.CockpitHelpPage {
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
