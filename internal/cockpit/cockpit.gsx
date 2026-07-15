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
// own feature drafts, submit lifecycle, route mutation, target mutation, or run
// execution.
type Cockpit struct {
	Ctx            context.Context
	Model          readmodel.CockpitReadModel
	ActiveTabIndex *tui.State[int]
	RefreshNotice  *tui.State[readmodel.Notice]
	WorkspacePages map[readmodel.WorkspaceID]*workspace_page.PageView
	WorkspacePage  *workspace_page.PageView
	HelpPage       *help_page.ViewView
	WorkspacePorts ports.WorkspaceCommands
	WorkspaceQuery ports.WorkspaceQueries
}

// NewCockpit constructs the root shell from an already-loaded readmodel.
func NewCockpit(model readmodel.CockpitReadModel) *Cockpit {
	return NewCockpitWithContext(model, context.Background(), nil, nil)
}

func NewCockpitWithWorkspacePorts(model readmodel.CockpitReadModel, query ports.WorkspaceQueries, commands ports.WorkspaceCommands) *Cockpit {
	return NewCockpitWithContext(model, context.Background(), query, commands)
}

func NewCockpitWithContext(model readmodel.CockpitReadModel, ctx context.Context, query ports.WorkspaceQueries, commands ports.WorkspaceCommands) *Cockpit {
	if ctx == nil {
		ctx = context.Background()
	}
	activeTab := selectedTabIndex(model.Tabs)
	cockpit := &Cockpit{
		Ctx:            ctx,
		Model:          model,
		ActiveTabIndex: tui.NewState(activeTab),
		RefreshNotice:  tui.NewState(readmodel.Notice{}),
		HelpPage:       help_page.View(model.Help),
		WorkspacePorts: commands,
		WorkspaceQuery: query,
	}
	cockpit.WorkspacePages = cockpit.workspacePagesByTab(model)
	cockpit.WorkspacePage = cockpit.initialWorkspacePage(model, activeTab)
	return cockpit
}

func (c *Cockpit) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnPreemptStop(tui.KeyTab, c.activateNextTab),
		tui.OnPreemptStop(tui.KeyTab.Shift(), c.activatePreviousTab),
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
	if index, ok := helpTabIndex(c.Model.Tabs); ok {
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
	if c.WorkspaceQuery != nil {
		ctx, cancel := c.refreshContext()
		defer cancel()
		if fresh, err := c.WorkspaceQuery.LoadCockpit(ctx); err == nil {
			workspace, workspaceErr := c.WorkspaceQuery.LoadWorkspace(ctx, saved.ID)
			if workspaceErr != nil {
				workspace = saved
				c.RefreshNotice.Set(staleRefreshNotice("refresh stale: saved workspace shown; " + workspaceErr.Error()))
			} else {
				c.RefreshNotice.Set(readmodel.Notice{})
			}
			c.replaceModel(selectWorkspace(updateWorkspaceInModel(fresh, workspace), workspace.ID))
			return
		} else {
			c.RefreshNotice.Set(staleRefreshNotice("refresh stale: saved workspace shown; " + err.Error()))
		}
	}
	c.replaceModel(selectWorkspace(updateWorkspaceInModel(c.Model, saved), saved.ID))
}

func (c *Cockpit) refreshAfterWorkspaceDelete(deleted readmodel.WorkspaceID) {
	if c.WorkspaceQuery != nil {
		ctx, cancel := c.refreshContext()
		defer cancel()
		if fresh, err := c.WorkspaceQuery.LoadCockpit(ctx); err == nil {
			c.RefreshNotice.Set(readmodel.Notice{})
			c.replaceModel(removeWorkspaceFromModel(fresh, deleted))
			return
		} else {
			c.RefreshNotice.Set(staleRefreshNotice("refresh stale: deleted workspace hidden; " + err.Error()))
		}
	}
	c.replaceModel(removeWorkspaceFromModel(c.Model, deleted))
}

func (c *Cockpit) refreshContext() (context.Context, context.CancelFunc) {
	base := c.Ctx
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, cockpitRefreshTimeout)
}

func (c *Cockpit) replaceModel(model readmodel.CockpitReadModel) {
	activeTab := selectedTabIndex(model.Tabs)
	previousPages := c.WorkspacePages
	c.Model = model
	c.ActiveTabIndex.Set(activeTab)
	c.WorkspacePages = c.workspacePagesByTab(model)
	c.preserveDraftWorkspacePages(previousPages, model)
	c.WorkspacePage = c.initialWorkspacePage(model, activeTab)
	c.HelpPage = help_page.View(model.Help)
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
			@c.WorkspacePage
		}
		<hr />
		@ShellFooter(c.activeModel())
	</div>
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
	model.ActivePage = readmodel.CockpitHelpPage
	if index, ok := helpTabIndex(model.Tabs); ok {
		model.Tabs[index].Selected = true
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
