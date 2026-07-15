package workspace

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	activitysection "github.com/swobuforge/swobu/internal/cockpit/sections/activity"
	routessection "github.com/swobuforge/swobu/internal/cockpit/sections/routes"
	overviewsection "github.com/swobuforge/swobu/internal/cockpit/sections/workspace_overview"
)

type PageView struct {
	OverviewSection  *overviewsection.SectionView
	RoutesSection    *routessection.SectionView
	ActivitySection  *activitysection.SectionView
}

func Page(workspace readmodel.WorkspaceReadModel) *PageView {
	return &PageView{
		OverviewSection:  overviewsection.Section(workspace),
		RoutesSection:    routessection.Section(workspace),
		ActivitySection:  activitysection.Section(workspace),
	}
}

func (v *PageView) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyUp, v.focusPrevious),
		tui.OnStop(tui.KeyDown, v.focusNext),
		tui.OnStop(tui.KeyEnter, v.activateFocused),
		tui.OnStop(tui.KeyEscape, v.backOut),
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

func (v *PageView) activateFocused(event tui.KeyEvent) {
	app := event.App()
	if app == nil || app.Focused() == nil {
		return
	}
	if element, ok := app.Focused().(*tui.Element); ok {
		element.Activate()
	}
}

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
