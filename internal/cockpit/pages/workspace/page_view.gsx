package workspace

import (
	"context"
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	activitysection "github.com/swobuforge/swobu/internal/cockpit/sections/activity"
	routessection "github.com/swobuforge/swobu/internal/cockpit/sections/routes"
	overviewsection "github.com/swobuforge/swobu/internal/cockpit/sections/workspace_overview"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type PageView struct {
	OverviewSection    *overviewsection.SectionView
	RoutesSection      *routessection.SectionView
	ActivitySection    *activitysection.SectionView
	OnWorkspaceSaved   func(readmodel.WorkspaceReadModel)
	OnWorkspaceDeleted func(readmodel.WorkspaceID)
}

// Page composes one workspace surface from explicit ports.
//
// Target setup and auth capabilities are supplied separately so the page does
// not have to rediscover optional adapter interfaces from WorkspaceCommands.
func Page(workspace readmodel.WorkspaceReadModel, commands ports.WorkspaceCommands, setupQueries ports.TargetSetupQueries, authCommands ports.TargetAuthCommands, ctx context.Context, activityQuery ports.ActivityQueries) *PageView {
	page := &PageView{
		OverviewSection:  overviewsection.Section(workspace, commands),
		RoutesSection:    routessection.Section(workspace, routeCommandPort(commands)),
		ActivitySection:  activitysection.Section(workspace, ctx, activityQuery),
	}
	if setupQueries != nil {
		page.RoutesSection.ListProviders = setupQueries.ListTargetProviders
		page.RoutesSection.TargetSetupQueries = setupQueries
	}
	if authCommands != nil {
		page.RoutesSection.TargetAuthCommands = authCommands
	}
	page.OverviewSection.OnWorkspaceSaved = page.workspaceSaved
	page.OverviewSection.OnWorkspaceDeleted = page.workspaceDeleted
	return page
}

func routeCommandPort(commands ports.WorkspaceCommands) ports.RouteCommands {
	if commands == nil {
		return nil
	}
	if routes, ok := any(commands).(ports.RouteCommands); ok {
		return routes
	}
	return nil
}

func (v *PageView) workspaceSaved(workspace readmodel.WorkspaceReadModel) {
	if v.OnWorkspaceSaved != nil {
		v.OnWorkspaceSaved(workspace)
	}
}

func (v *PageView) workspaceDeleted(workspaceID readmodel.WorkspaceID) {
	if v.OnWorkspaceDeleted != nil {
		v.OnWorkspaceDeleted(workspaceID)
	}
}

func (v *PageView) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyUp, v.focusPrevious),
		tui.OnStop(tui.KeyDown, v.focusNext),
		tui.OnStop(tui.KeyEscape, v.backOut),
		tui.OnStop(tui.KeyEnter, ui.ActivateFocusedElement),
	}
}

func (v *PageView) focusPrevious(event tui.KeyEvent) {
	if app := event.App(); app != nil {
		app.FocusPrev()
	}
}

func (v *PageView) focusNext(event tui.KeyEvent) {
	if app := event.App(); app != nil {
		app.FocusNext()
	}
}

// swobu:lint ignore tui-parent-calls-child-method because=staged refactor: replace imperative Back() with parent-owned ActiveInline state enum per go-tui-canons anti-pattern #7
func (v *PageView) backOut(event tui.KeyEvent) {
	if v.OverviewSection.Back() {
		return
	}
	if v.RoutesSection.Back() {
		return
	}
	v.ActivitySection.Back()
}

templ (v *PageView) Render() {
	<div class="flex-col w-full">
		@v.OverviewSection
		<br />
		@v.RoutesSection
		<br />
		@v.ActivitySection
		<br />
		<br />
	</div>
}
