package activity

import (
	"context"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// SectionView renders a single static activity row under an always-expanded
// header. V0: no click-through, no toggle, no focusable rows.
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

func (s *SectionView) activityText() string {
	activity := s.Workspace.Activity
	if s.ActivityQuery != nil && !s.Workspace.IsDraft() {
		q, err := s.ActivityQuery.ListActivity(s.Ctx, ports.ListActivityRequest{WorkspaceID: s.Workspace.ID, Limit: 1})
		if err == nil {
			activity = q
		}
	}
	latest, ok := activity.LatestRow()
	if !ok {
		return ""
	}
	return latest.RowValue()
}

func (s *SectionView) visibleActivityText() string {
	if text := s.activityText(); text != "" {
		return text
	}
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
			@ActivityRow("latest", s.visibleActivityText())
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
