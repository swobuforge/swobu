package cockpit

import (
	"context"
	"time"

	tui "github.com/grindlemire/go-tui"

	help_page "github.com/swobuforge/swobu/internal/cockpit/pages/help"
	workspace_page "github.com/swobuforge/swobu/internal/cockpit/pages/workspace"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

const cockpitRefreshTimeout = 5 * time.Second

// Cockpit composes the operator shell, active page, and static global frame.
//
// It owns shell composition and selected top-level readmodel data. It does not
// own feature drafts, submit lifecycle, route mutation, target mutation, run
// execution, or model refresh policy.
type Cockpit struct {
	Model          readmodel.CockpitReadModel
	ActiveTabIndex *tui.State[int]
	RefreshNotice  *tui.State[readmodel.Notice]
	Reloader       *ModelReloader
	WorkspacePages map[readmodel.WorkspaceID]*workspace_page.PageView
	WorkspacePage  *workspace_page.PageView
	HelpPage       *help_page.PageView
	Context        context.Context
	WorkspacePorts ports.WorkspaceCommands
	WorkspaceQuery ports.WorkspaceQueries
	HelpActions    ports.HelpActions
}

// NewCockpit constructs the root shell from an already-loaded readmodel.
func NewCockpit(model readmodel.CockpitReadModel) *Cockpit {
	return NewCockpitWithContext(model, context.Background(), nil, nil)
}

func NewCockpitWithContext(model readmodel.CockpitReadModel, ctx context.Context, query ports.WorkspaceQueries, commands ports.WorkspaceCommands) *Cockpit {
	if ctx == nil {
		ctx = context.Background()
	}
	activeTab := selectedTabIndex(model.Tabs)
	helpActions, _ := any(commands).(ports.HelpActions)
	cockpit := &Cockpit{
		Model:           model,
		ActiveTabIndex:  tui.NewState(activeTab),
		RefreshNotice:   tui.NewState(readmodel.Notice{}),
		Reloader:        NewModelReloader(ctx, query, cockpitRefreshTimeout),
		HelpPage:        help_page.ViewWithContext(model.Help, ctx, helpActions),
		Context:         ctx,
		WorkspacePorts:  commands,
		WorkspaceQuery:  query,
		HelpActions:     helpActions,
	}
	cockpit.WorkspacePages = cockpit.workspacePagesByTab(model)
	cockpit.WorkspacePage = cockpit.initialWorkspacePage(model, activeTab)
	return cockpit
}

func (c *Cockpit) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnPreemptStop(tui.KeyTab, c.activateNextTab),
		tui.OnPreemptStop(tui.KeyTab.Shift(), c.activatePreviousTab),
		tui.OnStop(tui.KeyF1, c.activateHelpTab),
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
	if focusedTextEditor(event) {
		return
	}
	if index, ok := helpTabIndex(c.Model.Tabs); ok {
		c.activateTab(index)
	}
}

func focusedTextEditor(event tui.KeyEvent) bool {
	app := event.App()
	if app == nil {
		return false
	}
	switch app.Focused().(type) {
	case *tui.Input, *tui.TextArea:
		return true
	case *tui.Element:
		focused := app.Focused().(*tui.Element)
		switch focused.Component().(type) {
		case *tui.Input, *tui.TextArea:
			return true
		}
	default:
		return false
	}
	return false
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
		c.WorkspacePages = c.workspacePagesByTab(c.Model)
	}
	if page := c.WorkspacePages[model.SelectedWorkspaceID]; page != nil {
		return page
	}
	page := c.workspacePage(model.SelectedWorkspace)
	c.WorkspacePages[model.SelectedWorkspaceID] = page
	return page
}

func (c *Cockpit) refreshAfterWorkspaceSave(saved readmodel.WorkspaceReadModel) {
	model, notice := c.Reloader.RefreshAfterSave(c.Model, saved)
	c.RefreshNotice.Set(notice)
	c.replaceModel(model)
}

func (c *Cockpit) refreshAfterWorkspaceDelete(deleted readmodel.WorkspaceID) {
	model, notice := c.Reloader.RefreshAfterDelete(c.Model, deleted)
	c.RefreshNotice.Set(notice)
	c.replaceModel(model)
}

func (c *Cockpit) replaceModel(model readmodel.CockpitReadModel) {
	activeTab := selectedTabIndex(model.Tabs)
	previousPages := c.WorkspacePages
	c.Model = model
	c.ActiveTabIndex.Set(activeTab)
	c.WorkspacePages = c.workspacePagesByTab(model)
	c.preserveDraftWorkspacePages(previousPages, model)
	c.WorkspacePage = c.initialWorkspacePage(model, activeTab)
	c.HelpPage = help_page.ViewWithContext(model.Help, c.Context, c.HelpActions)
}

// preserveDraftWorkspacePages carries unsaved [+] workflow state across a
// daemon-backed refresh. Saved workspace workflows intentionally remount after
// mutation so their visible state matches the reloaded operator projection.
func (c *Cockpit) preserveDraftWorkspacePages(previous map[readmodel.WorkspaceID]*workspace_page.PageView, model readmodel.CockpitReadModel) {
	if previous == nil {
		return
	}
	for _, tab := range model.Tabs {
		if tab.Kind != readmodel.WorkspaceTabDraft {
			continue
		}
		if page := previous[tab.ID]; page != nil {
			c.WorkspacePages[tab.ID] = page
		}
	}
}

templ (c *Cockpit) Render() {
	<div class="flex-col h-full w-full" deps={c.ActiveTabIndex, c.RefreshNotice}>
		@ShellHeader(c.activeModel())
		if c.RefreshNotice.Get().Visible() {
			@RefreshNotice(c.RefreshNotice.Get())
		}
		<hr />
		if c.activeModel().ActivePage == readmodel.CockpitHelpPage {
			@c.HelpPage
		} else {
			if app != nil {
				@ActiveWorkspacePage(c)
			} else {
				@c.WorkspacePage
			}
		}
		<hr />
		@ShellFooter(c.activeModel())
	</div>
}

func ActiveWorkspacePage(c *Cockpit) *workspace_page.PageView {
	return c.WorkspacePage
}

templ RefreshNotice(notice readmodel.Notice) {
	<div class="flex-row w-full">
		<span>{notice.Message}</span>
	</div>
}

func staleRefreshNotice(message string) readmodel.Notice {
	return readmodel.Notice{Kind: readmodel.NoticeStale, Message: message}
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
		if model.HeaderRight != "" {
			<span>{model.HeaderRight}</span>
		}
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
			<span>F1 help</span>
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

func (c *Cockpit) workspacePagesByTab(model readmodel.CockpitReadModel) map[readmodel.WorkspaceID]*workspace_page.PageView {
	pages := make(map[readmodel.WorkspaceID]*workspace_page.PageView, len(model.Tabs))
	for _, tab := range model.Tabs {
		if tab.Kind == readmodel.WorkspaceTabHelp {
			continue
		}
		workspace := workspaceForTab(model, tab)
		pages[tab.ID] = c.workspacePage(workspace)
	}
	return pages
}

func (c *Cockpit) workspacePage(workspace readmodel.WorkspaceReadModel) *workspace_page.PageView {
	page := workspace_page.Page(workspace, c.WorkspacePorts)
	page.OnWorkspaceSaved = c.refreshAfterWorkspaceSave
	page.OnWorkspaceDeleted = c.refreshAfterWorkspaceDelete
	return page
}

func (c *Cockpit) initialWorkspacePage(model readmodel.CockpitReadModel, activeTab int) *workspace_page.PageView {
	if len(model.Tabs) > 0 {
		tab := model.Tabs[wrapTabIndex(activeTab, len(model.Tabs))]
		if tab.Kind != readmodel.WorkspaceTabHelp {
			return c.WorkspacePages[tab.ID]
		}
	}
	return c.WorkspacePages[model.SelectedWorkspaceID]
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

func selectWorkspace(model readmodel.CockpitReadModel, id readmodel.WorkspaceID) readmodel.CockpitReadModel {
	for i := range model.Tabs {
		selected := model.Tabs[i].ID == id
		model.Tabs[i].Selected = selected
		if selected {
			model.SelectedWorkspaceID = model.Tabs[i].ID
			model.ActivePage = readmodel.CockpitWorkspacePage
			model.SelectedWorkspace = workspaceForTab(model, model.Tabs[i])
		}
	}
	return model
}

func updateWorkspaceInModel(model readmodel.CockpitReadModel, workspace readmodel.WorkspaceReadModel) readmodel.CockpitReadModel {
	replaced := false
	for i := range model.Tabs {
		if model.Tabs[i].ID != workspace.ID {
			continue
		}
		model.Tabs[i].Slug = workspace.Slug
		replaced = true
		break
	}
	if !replaced {
		model.Tabs = append(model.Tabs, readmodel.WorkspaceTabReadModel{
			ID:   workspace.ID,
			Slug: workspace.Slug,
			Kind: readmodel.WorkspaceTabExisting,
		})
	}
	model.SelectedWorkspaceID = workspace.ID
	model.SelectedWorkspace = workspace
	return model
}

func removeWorkspaceFromModel(model readmodel.CockpitReadModel, deleted readmodel.WorkspaceID) readmodel.CockpitReadModel {
	tabs := make([]readmodel.WorkspaceTabReadModel, 0, len(model.Tabs))
	for _, tab := range model.Tabs {
		if tab.ID != deleted {
			tab.Selected = false
			tabs = append(tabs, tab)
		}
	}
	model.Tabs = tabs
	for i := range model.Tabs {
		if model.Tabs[i].Kind != readmodel.WorkspaceTabExisting {
			continue
		}
		model.Tabs[i].Selected = true
		model.SelectedWorkspaceID = model.Tabs[i].ID
		model.ActivePage = readmodel.CockpitWorkspacePage
		model.SelectedWorkspace = workspaceForTab(model, model.Tabs[i])
		return model
	}
	model.SelectedWorkspaceID = ""
	model.SelectedWorkspace = readmodel.WorkspaceReadModel{}
	model.ActivePage = readmodel.CockpitWorkspacePage
	if index, ok := draftTabIndex(model.Tabs); ok {
		model.Tabs[index].Selected = true
		model.SelectedWorkspaceID = model.Tabs[index].ID
		model.SelectedWorkspace = workspaceForTab(model, model.Tabs[index])
	}
	return model
}

func selectedTabIndex(tabs []readmodel.WorkspaceTabReadModel) int {
	for i, tab := range tabs {
		if tab.Selected {
			return i
		}
	}
	return 0
}

func helpTabIndex(tabs []readmodel.WorkspaceTabReadModel) (int, bool) {
	for i, tab := range tabs {
		if tab.Kind == readmodel.WorkspaceTabHelp {
			return i, true
		}
	}
	return 0, false
}

func draftTabIndex(tabs []readmodel.WorkspaceTabReadModel) (int, bool) {
	for i, tab := range tabs {
		if tab.Kind == readmodel.WorkspaceTabDraft {
			return i, true
		}
	}
	return 0, false
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
