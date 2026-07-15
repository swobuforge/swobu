package workspace_plane

import (
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	activitysection "github.com/swobuforge/swobu/internal/cockpit/sections/activity"
	routessection "github.com/swobuforge/swobu/internal/cockpit/sections/routes"
	workspacesection "github.com/swobuforge/swobu/internal/cockpit/sections/workspace"
)

type ViewModel struct {
	WorkspaceSection *workspacesection.SectionView
	RoutesSection    *routessection.SectionView
	ActivitySection  *activitysection.SectionView
}

func View(workspace readmodel.WorkspaceReadModel) *ViewModel {
	return &ViewModel{
		WorkspaceSection: workspacesection.Section(workspace),
		RoutesSection:    routessection.Section(workspace),
		ActivitySection:  activitysection.Section(workspace),
	}
}

templ (v *ViewModel) Render() {
	<div class="flex-col w-full">
		@v.WorkspaceSection
		<br />
		@v.RoutesSection
		<br />
		@v.ActivitySection
		<br />
		<br />
	</div>
}
