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
	OverviewSection *overviewsection.SectionView
	RoutesSection   *routessection.SectionView
	ActivitySection *activitysection.SectionView
	OnWorkspaceSaved       func(readmodel.WorkspaceReadModel)
	OnWorkspaceDeleted     func(readmodel.WorkspaceID)
	OnWorkspaceDiscarded   func()
	OnNotice               func(readmodel.Notice)
}

// Page composes one workspace surface from explicit ports.
//
// Ordinary drafts render only the overview/create flow until named. Bootstrap
// and persisted workspaces render overview, routes, and activity; bootstrap's
// Activity section remains local and never starts the persisted query lifecycle.
//
// Target setup and auth capabilities are supplied separately so the page does
// not have to rediscover optional adapter interfaces from WorkspaceCommands.
func Page(workspace readmodel.WorkspaceReadModel, commands ports.WorkspaceCommands, setupQueries ports.TargetSetupQueries, authCommands ports.TargetAuthCommands, ctx context.Context, activityQuery ports.ActivityQueries, credentialCommands ports.TargetCredentialCommands) *PageView {
	page := &PageView{
		OverviewSection: overviewsection.Section(workspace, commands),
		RoutesSection:   routessection.Section(workspace, routeCommandPort(commands)),
		ActivitySection: activitysection.Section(workspace, ctx, activityQuery),
	}
	if setupQueries != nil {
		page.RoutesSection.TargetConfigs.Commands.Setup = setupQueries
	}
	if authCommands != nil {
		page.RoutesSection.TargetConfigs.Commands.Auth = authCommands
	}
	if credentialCommands != nil {
		page.RoutesSection.TargetConfigs.Commands.Credentials = credentialCommands
	}
	page.OverviewSection.OnWorkspaceSaved = page.workspaceSaved
	page.OverviewSection.OnWorkspaceDeleted = page.workspaceDeleted
	page.OverviewSection.OnWorkspaceDiscarded = page.workspaceDiscarded
	page.OverviewSection.OnNotice = page.publishNotice
	page.RoutesSection.OnWorkspacePersisted = page.workspacePersisted
	return page
}

func (v *PageView) publishNotice(notice readmodel.Notice) {
	if v.OnNotice != nil {
		v.OnNotice(notice)
	}
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
	requestAddRouteFocusAfterSave(workspace)
	if v.OnWorkspaceSaved != nil {
		v.OnWorkspaceSaved(workspace)
	}
}

func (v *PageView) workspaceDeleted(workspaceID readmodel.WorkspaceID) {
	if v.OnWorkspaceDeleted != nil {
		v.OnWorkspaceDeleted(workspaceID)
	}
}

func (v *PageView) workspaceDiscarded() {
	if v.OnWorkspaceDiscarded != nil {
		v.OnWorkspaceDiscarded()
	}
}

func (v *PageView) workspacePersisted(workspace readmodel.WorkspaceReadModel) {
	if v.OnWorkspaceSaved != nil {
		v.OnWorkspaceSaved(workspace)
	}
}

func (v *PageView) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyUp, v.selectPrevious),
		tui.OnStop(tui.KeyDown, v.selectNext),
		tui.OnStop(tui.KeyEscape, v.backOut),
		tui.OnStop(tui.KeyEnter, ui.ActivateCurrentSelection),
	}
}

func (v *PageView) selectPrevious(event tui.KeyEvent) {
	ui.SelectPrevious(event)
}

func (v *PageView) selectNext(event tui.KeyEvent) {
	ui.SelectNext(event)
}

func (v *PageView) backOut(event tui.KeyEvent) {
	if v.OverviewSection != nil && v.OverviewSection.Back() {
		return
	}
	if v.RoutesSection != nil && v.RoutesSection.Back() {
		return
	}
	// No workspace-owned semantic state consumed Escape, so it closes the app.
	if app := event.App(); app != nil {
		app.Stop()
	}
}

templ (v *PageView) Render() {
	<div class="flex-col w-full">
		@v.OverviewSection
		if !v.OverviewSection.Model.IsDraft() || v.OverviewSection.Model.Slug != "" {
			if consumeAddRouteFocusAfterSave(v.OverviewSection.Model) {
				v.RoutesSection.RequestAddRouteFocus()
			}
			<br />
			@v.RoutesSection
			if !v.OverviewSection.Model.IsDraft() {
				<br />
				@v.ActivitySection
			}
		}
		<br />
		<br />
	</div>
}
