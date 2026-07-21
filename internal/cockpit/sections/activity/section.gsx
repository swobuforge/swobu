package activity

import (
	"context"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// SectionView renders recent static activity rows under an always-expanded
// header. Rows are evidence, not controls: no click-through or focus state.
type SectionView struct {
	Workspace     readmodel.WorkspaceReadModel
	Ctx           context.Context
	ActivityQuery ports.ActivityQueries
}

func Section(workspace readmodel.WorkspaceReadModel, ctx context.Context, activityQuery ports.ActivityQueries) *SectionView {
	if ctx == nil {
		ctx = context.Background()
	}
	return &SectionView{
		Workspace:     workspace,
		Ctx:           ctx,
		ActivityQuery: activityQuery,
	}
}

func (s *SectionView) activityRows() []readmodel.ActivityRowReadModel {
	activity := s.Workspace.Activity
	if s.ActivityQuery != nil && !s.Workspace.IsDraft() {
		q, err := s.ActivityQuery.ListActivity(s.Ctx, ports.ListActivityRequest{WorkspaceID: s.Workspace.ID, Limit: 5})
		if err == nil {
			activity = q
		}
	}
	if len(activity.Rows) > 0 {
		return activity.Rows
	}
	latest, ok := activity.LatestRow()
	if !ok {
		return nil
	}
	return []readmodel.ActivityRowReadModel{latest}
}

func (s *SectionView) emptyActivityText() string {
	if s.Workspace.IsDraft() {
		return "(no activity)"
	}
	return "no requests yet"
}

var _ tui.Component = (*SectionView)(nil)

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		@ActivityHeader()
		<div class="pl-3 flex-col w-full">
			if rows := s.activityRows(); len(rows) > 0 {
				for index, row := range rows {
					if index == 0 {
						@ActivityRow("latest", row.RowValue())
					} else {
						@ActivityRow("", row.RowValue())
					}
				}
			} else {
				@ActivityRow("latest", s.emptyActivityText())
			}
		</div>
	</div>
}

templ ActivityHeader() {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span>activity ▾</span>
	</div>
}

templ ActivityRow(label string, value string) {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span class="w-18">{label}</span>
		<span>{value}</span>
	</div>
}
