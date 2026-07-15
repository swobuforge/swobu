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
	Model          readmodel.CockpitReadModel
	ActiveTabIndex *tui.State[int]
	WorkspacePages map[readmodel.WorkspaceID]*workspace_page.PageView
	WorkspacePage  *workspace_page.PageView
	HelpPage       *help_page.ViewView
}

// NewCockpit constructs the root shell from an already-loaded readmodel.
func NewCockpit(model readmodel.CockpitReadModel) *Cockpit {
	workspacePages := workspacePagesByTab(model)
	activeTab := selectedTabIndex(model.Tabs)
	return &Cockpit{
		Model:          model,
		ActiveTabIndex: tui.NewState(activeTab),
		WorkspacePages: workspacePages,
		WorkspacePage:  initialWorkspacePage(model, workspacePages, activeTab),
		HelpPage:       help_page.View(model.Help),
	}
}

func (c *Cockpit) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyTab, c.activateNextTab),
		tui.OnStop(tui.KeyTab.Shift(), c.activatePreviousTab),
		tui.OnStop(tui.Rune('?'), c.activateHelpTab),
		tui.OnStop(tui.Rune('q'), c.quit),
	}
}

func (c *Cockpit) activateNextTab(event tui.KeyEvent) {
	c.activateTab(c.activeTabIndex() + 1)
}

func (c *Cockpit) activatePreviousTab(event tui.KeyEvent) {
	c.activateTab(c.activeTabIndex() - 1)
}

func (c *Cockpit) activateHelpTab(event tui.KeyEvent) {
	if index, ok := c.helpTabIndex(); ok {
		c.activateTab(index)
	}
}

func (c *Cockpit) quit(event tui.KeyEvent) {
	if app := event.App(); app != nil {
		app.Stop()
	}
}

func (c *Cockpit) activateTab(index int) {
	if len(c.Model.Tabs) == 0 {
		return
	}
	index = wrapTabIndex(index, len(c.Model.Tabs))
	c.ActiveTabIndex.Set(index)
	if c.Model.Tabs[index].Kind != readmodel.WorkspaceTabHelp {
		model := c.activeModel()
		c.WorkspacePage = c.activeWorkspacePage(model)
	}
}

func (c *Cockpit) activeTabIndex() int {
	if c.ActiveTabIndex == nil {
		return selectedTabIndex(c.Model.Tabs)
	}
	return wrapTabIndex(c.ActiveTabIndex.Get(), len(c.Model.Tabs))
}

func (c *Cockpit) activeModel() readmodel.CockpitReadModel {
	model := c.Model
	index := c.activeTabIndex()
	for i := range model.Tabs {
		model.Tabs[i].Selected = i == index
	}
	if len(model.Tabs) == 0 {
		return model
	}

	tab := model.Tabs[index]
	model.SelectedWorkspaceID = tab.ID
	if tab.Kind == readmodel.WorkspaceTabHelp {
		model.ActivePage = readmodel.CockpitHelpPage
		return model
	}

	model.ActivePage = readmodel.CockpitWorkspacePage
	model.SelectedWorkspace = workspaceForTab(model, tab)
	return model
}

func (c *Cockpit) activeWorkspacePage(model readmodel.CockpitReadModel) *workspace_page.PageView {
	if c.WorkspacePages == nil {
		c.WorkspacePages = workspacePagesByTab(c.Model)
	}
	if page := c.WorkspacePages[model.SelectedWorkspaceID]; page != nil {
		return page
	}
	page := workspace_page.Page(model.SelectedWorkspace)
	c.WorkspacePages[model.SelectedWorkspaceID] = page
	return page
}

func (c *Cockpit) helpTabIndex() (int, bool) {
	for i, tab := range c.Model.Tabs {
		if tab.Kind == readmodel.WorkspaceTabHelp {
			return i, true
		}
	}
	return 0, false
}

templ (c *Cockpit) Render() {
	<div class="flex-col h-full w-full" deps={c.ActiveTabIndex}>
		@ShellHeader(c.activeModel())
		<hr />
		if c.activeModel().ActivePage == readmodel.CockpitHelpPage {
			@c.HelpPage
		} else {
			@c.WorkspacePage
		}
		<hr />
		@ShellFooter(c.activeModel())
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

func workspacePagesByTab(model readmodel.CockpitReadModel) map[readmodel.WorkspaceID]*workspace_page.PageView {
	pages := make(map[readmodel.WorkspaceID]*workspace_page.PageView, len(model.Tabs))
	for _, tab := range model.Tabs {
		if tab.Kind == readmodel.WorkspaceTabHelp {
			continue
		}
		workspace := workspaceForTab(model, tab)
		pages[tab.ID] = workspace_page.Page(workspace)
	}
	return pages
}

func initialWorkspacePage(model readmodel.CockpitReadModel, pages map[readmodel.WorkspaceID]*workspace_page.PageView, activeTab int) *workspace_page.PageView {
	if len(model.Tabs) > 0 {
		tab := model.Tabs[wrapTabIndex(activeTab, len(model.Tabs))]
		if tab.Kind != readmodel.WorkspaceTabHelp {
			return pages[tab.ID]
		}
	}
	return pages[model.SelectedWorkspaceID]
}

func workspaceForTab(model readmodel.CockpitReadModel, tab readmodel.WorkspaceTabReadModel) readmodel.WorkspaceReadModel {
	if tab.Kind == readmodel.WorkspaceTabDraft {
		return readmodel.WorkspaceReadModel{
			ID:    tab.ID,
			Slug:  "",
			State: readmodel.WorkspaceDraft,
		}
	}
	if tab.ID == model.SelectedWorkspaceID {
		return model.SelectedWorkspace
	}
	return readmodel.WorkspaceReadModel{
		ID:    tab.ID,
		Slug:  tab.Slug,
		State: readmodel.WorkspaceExisting,
	}
}

func selectedTabIndex(tabs []readmodel.WorkspaceTabReadModel) int {
	for i, tab := range tabs {
		if tab.Selected {
			return i
		}
	}
	return 0
}

func wrapTabIndex(index int, tabCount int) int {
	if tabCount <= 0 {
		return 0
	}
	index %= tabCount
	if index < 0 {
		index += tabCount
	}
	return index
}
