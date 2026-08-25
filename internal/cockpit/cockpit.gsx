package cockpit

import (
	"context"
	"time"

	tui "github.com/grindlemire/go-tui"

	help_page "github.com/swobuforge/swobu/internal/cockpit/pages/help"
	workspace_page "github.com/swobuforge/swobu/internal/cockpit/pages/workspace"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

const cockpitRefreshTimeout = 5 * time.Second

// Cockpit composes the operator shell, active page, and static global frame.
//
// It owns shell composition, body viewport clipping, and selected top-level
// readmodel data. It does not own feature drafts, submit lifecycle, route
// mutation, target mutation, or model refresh policy.
type Cockpit struct {
	Model          readmodel.CockpitReadModel
	ActiveTabIndex *tui.State[int]
	Notice         *tui.State[readmodel.Notice]
	BodyViewport   *ui.Viewport
	Reloader       *ModelReloader
	WorkspacePages map[readmodel.WorkspaceID]*workspace_page.PageView
	HelpPage       *help_page.PageView
	Context        context.Context
	WorkspacePorts ports.WorkspaceCommands
	WorkspaceQuery ports.WorkspaceQueries
}

// NewCockpit constructs the root shell from an already-loaded readmodel.
func NewCockpit(model readmodel.CockpitReadModel) *Cockpit {
	return NewCockpitWithContext(model, context.Background(), nil, nil)
}

func NewCockpitWithContext(model readmodel.CockpitReadModel, ctx context.Context, query ports.WorkspaceQueries, commands ports.WorkspaceCommands) *Cockpit {
	if ctx == nil {
		ctx = context.Background()
	}
	model = ensureWorkspaceRegistry(model)
	activeTab := selectedTabIndex(model.Tabs)
	cockpit := &Cockpit{
		Model:           model,
		ActiveTabIndex:  tui.NewState(activeTab),
		Notice:          tui.NewState(readmodel.Notice{}),
		BodyViewport:    &ui.Viewport{Ref: tui.NewRef(), ScrollY: tui.NewState(0), FollowFocused: true, MarginRows: 2},
		Reloader:        NewModelReloader(ctx, query, cockpitRefreshTimeout),
		HelpPage:        help_page.View(model.Help),
		Context:         ctx,
		WorkspacePorts:  commands,
		WorkspaceQuery:  query,
	}
	cockpit.WorkspacePages = cockpit.workspacePagesByTab(model)
	return cockpit
}

func (c *Cockpit) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnPreemptStop(tui.KeyCtrlC, c.quit),
		tui.OnPreemptStop(tui.KeyTab, c.activateNextTab),
		tui.OnPreemptStop(tui.KeyTab.Shift(), c.activatePreviousTab),
		tui.OnStop(tui.KeyF1, c.activateHelpTab),
		tui.OnStop(tui.KeyPageUp, func(event tui.KeyEvent) { c.BodyViewport.Page(-1) }),
		tui.OnStop(tui.KeyPageDown, func(event tui.KeyEvent) { c.BodyViewport.Page(1) }),
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

func (c *Cockpit) BindApp(app *tui.App) {
	c.bindAppFields(app)
	c.BodyViewport.BindApp(app)
}

func (c *Cockpit) currentWorkspacePage() *workspace_page.PageView {
	model := c.activeModel()
	if model.ActivePage != readmodel.CockpitWorkspacePage {
		return nil
	}
	return c.activeWorkspacePage(model)
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
	c.BodyViewport.Reset()
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
	if tab.Kind == readmodel.WorkspaceTabHelp {
		model.SelectedWorkspaceID = tab.ID
		model.ActivePage = readmodel.CockpitHelpPage
		return model
	}

	workspace := workspaceForTab(model, tab)
	model.SelectedWorkspaceID = tab.ID
	model.ActivePage = readmodel.CockpitWorkspacePage
	model.SelectedWorkspace = workspace
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

func activityQueryPort(query ports.WorkspaceQueries) ports.ActivityQueries {
	if query == nil {
		return nil
	}
	if activity, ok := any(query).(ports.ActivityQueries); ok {
		return activity
	}
	return nil
}

func (c *Cockpit) refreshAfterWorkspaceSave(saved readmodel.WorkspaceReadModel) {
	active := c.activeModel()
	wasOnboarding := active.SelectedWorkspace.IsOnboarding()
	model, notice := c.Reloader.RefreshAfterSave(active, saved)
	c.Notice.Set(notice)
	c.replaceModel(model, !wasOnboarding)
}

func (c *Cockpit) refreshWorkspaceProjection(workspace readmodel.WorkspaceReadModel) {
	model := c.activeModel()
	model = updateWorkspaceInModel(model, workspace)
	c.Model = model
}

func (c *Cockpit) refreshAfterWorkspaceDelete(deleted readmodel.WorkspaceID) {
	model, notice := c.Reloader.RefreshAfterDelete(c.activeModel(), deleted)
	c.Notice.Set(notice)
	c.replaceModel(model, true)
}

func (c *Cockpit) publishNotice(notice readmodel.Notice) {
	c.Notice.Set(notice)
}

// discardWorkspaceDraft resets the selected [+] onboarding draft without
// crossing the daemon boundary. Draft input is Cockpit-owned until the first
// target is committed atomically.
func (c *Cockpit) discardWorkspaceDraft() {
	model := c.activeModel()
	for i := range model.Tabs {
		if model.Tabs[i].Kind == readmodel.WorkspaceTabDraft {
			model.Tabs[i].Slug = ""
		}
	}
	model.SelectedWorkspaceID = "+"
	model.SelectedWorkspace = model.SelectedWorkspace.ResetDraftInput()
	c.replaceModel(model, false)
}

func (c *Cockpit) replaceModel(model readmodel.CockpitReadModel, preserveDraftPages bool) {
	activeTab := selectedTabIndex(model.Tabs)
	previousPages := c.WorkspacePages
	c.Model = model
	c.ActiveTabIndex.Set(activeTab)
	c.BodyViewport.Reset()
	c.WorkspacePages = c.workspacePagesByTab(model)
	// Persisted workspace pages are rebuilt from the authoritative registry.
	// Only draft pages retain local authoring state across refreshes.
	if preserveDraftPages {
		c.preserveDraftWorkspacePages(previousPages, model)
	}
	c.HelpPage = help_page.View(model.Help)
}

// preserveDraftWorkspacePages carries unsaved [+] workflow state across a
// daemon-backed refresh. If the draft page has already been promoted, it must
// remount as a fresh draft page instead of leaking the promoted values back
// into the [+] slot.
func (c *Cockpit) preserveDraftWorkspacePages(previous map[readmodel.WorkspaceID]*workspace_page.PageView, model readmodel.CockpitReadModel) {
	if previous == nil {
		return
	}
	for _, tab := range model.Tabs {
		if tab.Kind != readmodel.WorkspaceTabDraft {
			continue
		}
		page := previous[tab.ID]
		if page == nil || !page.OverviewSection.Model.IsDraft() {
			continue
		}
		c.WorkspacePages[tab.ID] = page
	}
}

// ActiveHelpPage returns the cached help page through a constructor-shaped
// expression. GSX mounts function-call components; direct stored-component
// rendering bypasses the child KeyMap even when the child is app-bound.
func ActiveHelpPage(c *Cockpit) *help_page.PageView {
	return c.HelpPage
}

// ActiveWorkspacePage returns the cached active workspace page through the same
// constructor-shaped path so generated go-tui dispatch includes page keys.
func ActiveWorkspacePage(c *Cockpit, model readmodel.CockpitReadModel) *workspace_page.PageView {
	if model.ActivePage != readmodel.CockpitWorkspacePage {
		return nil
	}
	return c.activeWorkspacePage(model)
}

func activeWorkspaceMountKey(model readmodel.CockpitReadModel) string {
	key := "workspace-page:" + string(model.SelectedWorkspaceID)
	// Projected and persisted `default` deliberately share product identity but
	// not component lifetime. Promotion must remount persisted-only Activity and
	// discard onboarding workflow state.
	if model.SelectedWorkspace.IsBootstrap() {
		return key + ":bootstrap"
	}
	if model.SelectedWorkspace.IsDraft() && model.SelectedWorkspace.Slug != "" {
		return key + ":" + model.SelectedWorkspace.Slug
	}
	return key
}

templ (c *Cockpit) Render() {
	<div class="flex-col h-full w-full" deps={c.ActiveTabIndex, c.Notice}>
		@ShellHeader(c.activeModel())
		if c.Notice.Get().Visible() {
			@ShellNotice(c.Notice.Get())
		}
		<hr />
		@CockpitBodyViewport(c)
		<hr />
		@ShellFooter(c.activeModel())
	</div>
}

func staleRefreshNotice(message string) readmodel.Notice {
	return readmodel.Notice{Kind: readmodel.NoticeStale, Message: message}
}

type FlowTextView = ui.FlowTextView

func FlowText(text string) *FlowTextView {
	return ui.FlowText(text)
}

templ ShellNotice(notice readmodel.Notice) {
	<div class="w-full">
		@FlowText(notice.Message)
	</div>
}

templ ShellHeader(model readmodel.CockpitReadModel) {
	<div class="flex-row w-full">
		<span class="w-9 font-bold nowrap">⛉ SWOBU</span>
		<div class="flex-row gap-1 grow justify-end">
			for _, tab := range model.Tabs {
				if tab.Selected {
					<span class="truncate nowrap" minWidth={0}>{activeTabLabel(tab)}</span>
				} else {
					<span class="truncate nowrap" minWidth={0}>{inactiveTabLabel(tab)}</span>
				}
			}
		</div>
		if model.HeaderRight != "" {
			<span class="ml-2 truncate nowrap" minWidth={0}>{model.HeaderRight}</span>
		}
	</div>
}

templ ShellFooter(model readmodel.CockpitReadModel) {
	<div class="flex-row gap-3">
		<span>↑↓ move</span>
		<span>↵ action</span>
		<span>? help</span>
		<span>esc back</span>
		<span>tab switch</span>
	</div>
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
	page := workspace_page.Page(
		workspace,
		c.WorkspacePorts,
		targetSetupQueriesPort(c.WorkspacePorts),
		targetAuthCommandsPort(c.WorkspacePorts),
		c.Context,
		activityQueryPort(c.WorkspaceQuery),
		targetCredentialCommandsPort(c.WorkspacePorts),
	)
	page.OnWorkspaceSaved = c.refreshAfterWorkspaceSave
	page.OnWorkspaceCommitted = c.refreshWorkspaceProjection
	page.OnWorkspaceDeleted = c.refreshAfterWorkspaceDelete
	page.OnWorkspaceDiscarded = c.discardWorkspaceDraft
	page.OnNotice = c.publishNotice
	return page
}

// The root shell owns optional adapter capability discovery. The workspace
// page receives explicit ports so it stays a pure composer.
func targetSetupQueriesPort(commands ports.WorkspaceCommands) ports.TargetSetupQueries {
	if commands == nil {
		return nil
	}
	if setupQueries, ok := any(commands).(ports.TargetSetupQueries); ok {
		return setupQueries
	}
	return nil
}

func targetAuthCommandsPort(commands ports.WorkspaceCommands) ports.TargetAuthCommands {
	if commands == nil {
		return nil
	}
	if authCommands, ok := any(commands).(ports.TargetAuthCommands); ok {
		return authCommands
	}
	return nil
}

func targetCredentialCommandsPort(commands ports.WorkspaceCommands) ports.TargetCredentialCommands {
	if commands == nil {
		return nil
	}
	if credentialCommands, ok := any(commands).(ports.TargetCredentialCommands); ok {
		return credentialCommands
	}
	return nil
}
