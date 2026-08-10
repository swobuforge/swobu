package activity

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	ActivitySnapshot *tui.State[readmodel.ActivityReadModel]
	RefreshInterval time.Duration
	RefreshLifecycle *activityRefreshLifecycle
}

func Section(workspace readmodel.WorkspaceReadModel, ctx context.Context, activityQuery ports.ActivityQueries) *SectionView {
	if ctx == nil {
		ctx = context.Background()
	}
	return &SectionView{
		Workspace:     workspace,
		Ctx:           ctx,
		ActivityQuery: activityQuery,
		ActivitySnapshot: tui.NewState(workspace.Activity),
		RefreshInterval: time.Second,
		RefreshLifecycle: &activityRefreshLifecycle{},
	}
}

func (s *SectionView) BindApp(app *tui.App) {
	s.bindAppFields(app)
	s.startActivityRefresh(app)
}

func (s *SectionView) UnbindApp() {
	s.stopActivityRefresh()
}

func (s *SectionView) activityRows() []readmodel.ActivityRowReadModel {
	activity := s.ActivitySnapshot.Get()
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

func activityObservedAt(row readmodel.ActivityRowReadModel) string {
	observedAt := strings.TrimSpace(row.ObservedAt) // swobu:io-string source=boundary
	if observedAt == "" {
		return "unknown"
	}
	return observedAt
}

func activityRoute(row readmodel.ActivityRowReadModel) string {
	if route := strings.TrimSpace(row.RouteLabel); route != "" { // swobu:io-string source=boundary
		return route
	}
	return string(row.RouteID)
}

func activityMiddleEvidence(row readmodel.ActivityRowReadModel) string {
	provider := strings.TrimSpace(row.ProviderSpec) // swobu:io-string source=boundary
	model := strings.TrimSpace(row.ProviderModel) // swobu:io-string source=boundary
	client := strings.TrimSpace(row.ClientLabel) // swobu:io-string source=boundary
	parts := make([]string, 0, 3)
	if provider != "" && model != "" {
		parts = append(parts, provider+"/"+model)
	}
	if client != "" {
		parts = append(parts, client)
	}
	if row.AttemptCount > 1 {
		parts = append(parts, fmt.Sprintf("%d attempts", row.AttemptCount))
	}
	return strings.Join(parts, " · ")
}

func activityStatusLabel(row readmodel.ActivityRowReadModel) string {
	if row.Status == readmodel.ActivityPending {
		return "…"
	}
	if row.HTTPStatus > 0 {
		return fmt.Sprint(row.HTTPStatus)
	}
	return ""
}

func activityDurationLabel(row readmodel.ActivityRowReadModel) string {
	if !row.DurationKnown || row.Status == readmodel.ActivityPending {
		return ""
	}
	duration := row.Duration
	switch {
	case duration < time.Second:
		duration = duration.Round(time.Millisecond)
	case duration < 10*time.Second:
		duration = duration.Round(100 * time.Millisecond)
	default:
		duration = duration.Round(time.Second)
	}
	return duration.String()
}

var _ tui.Component = (*SectionView)(nil)

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		@ActivityHeader()
		<div class="pl-3 flex-col w-full">
			if rows := s.activityRows(); len(rows) > 0 {
				for _, row := range rows {
					@ActivityRow(row)
				}
			} else {
				@ActivityColumns("", "", s.emptyActivityText(), "", "")
			}
		</div>
	</div>
}

templ ActivityHeader() {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span>activity</span>
	</div>
}

// ActivityRow composes one request into five left-aligned columns. Evidence is
// the sole elastic cell. Route receives enough fixed width for normal
// human-authored aliases because route identity outranks repeated client and
// attempt telemetry. Every row remains a single physical line.
templ ActivityRow(row readmodel.ActivityRowReadModel) {
	@ActivityColumns(activityObservedAt(row), activityRoute(row), activityMiddleEvidence(row), activityStatusLabel(row), activityDurationLabel(row))
}

templ ActivityColumns(observedAt string, route string, evidence string, status string, duration string) {
	<div class="flex-row flex-nowrap gap-2 w-full">
		<span class="w-8 shrink-0 nowrap truncate overflow-hidden">{observedAt}</span>
		<span class="w-18 shrink-0 min-w-0 nowrap truncate overflow-hidden">{route}</span>
		<span class="grow shrink min-w-0 nowrap truncate overflow-hidden">{evidence}</span>
		<span class="w-3 shrink-0 nowrap truncate overflow-hidden">{status}</span>
		<span class="w-7 shrink-0 nowrap truncate overflow-hidden">{duration}</span>
	</div>
}
