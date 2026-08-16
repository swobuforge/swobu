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

func activityRouteIdentity(row readmodel.ActivityRowReadModel) string {
	return strings.TrimSpace(string(row.RouteID)) // swobu:io-string source=boundary
}

func activityRouteLabel(row readmodel.ActivityRowReadModel) string {
	if route := strings.TrimSpace(row.RouteLabel); route != "" { // swobu:io-string source=boundary
		return route
	}
	return string(row.RouteID)
}

func activityTarget(row readmodel.ActivityRowReadModel) string {
	provider := strings.TrimSpace(row.ProviderSpec) // swobu:io-string source=boundary
	model := strings.TrimSpace(row.ProviderModel)   // swobu:io-string source=boundary
	if provider == "" || model == "" {
		return ""
	}
	return provider + "/" + model
}

func activityClientEvidence(row readmodel.ActivityRowReadModel) string {
	client := strings.TrimSpace(row.ClientLabel) // swobu:io-string source=boundary
	if client == "" {
		return ""
	}
	return "· " + client
}

func activityAttemptEvidence(row readmodel.ActivityRowReadModel) string {
	if row.AttemptCount <= 1 {
		return ""
	}
	return fmt.Sprintf("%d attempts", row.AttemptCount)
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
		<div class="flex-col w-full">
			if rows := s.activityRows(); len(rows) > 0 {
				for _, row := range rows {
					@ActivityRow(row, activityShowsClient(app))
				}
			} else {
				@ActivityColumns("", "", s.emptyActivityText(), "", "")
			}
		</div>
	</div>
}

func activityShowsClient(app *tui.App) bool {
	if app == nil {
		return false
	}
	width, _ := app.Size()
	return width >= 112
}

templ ActivityHeader() {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span>activity</span>
	</div>
}

// ActivityRow keeps path identities as independently truncatable flex children.
// Explicit outer gutters align the inert row with sibling section children and
// bound the inner flex width; padding on a full-width row would overflow before
// path evidence could yield. Client text shrinks first and attempt/outcome
// evidence keeps stable cells at the right.
templ ActivityRow(row readmodel.ActivityRowReadModel, showClient bool) {
	<div class="flex-row flex-nowrap w-full">
		<span class="w-5 shrink-0"></span>
		<div class="flex-row flex-nowrap flex-1 min-w-0">
		<span class="w-8 shrink-0 nowrap truncate overflow-hidden">{activityObservedAt(row)}</span>
		<span class="w-2 shrink-0"></span>
		if requested, route, target := activityPathParts(row); requested != "" || route != "" || target != "" {
			if requested != "" {
				<span class="flex-shrink-3 min-w-3 nowrap truncate overflow-hidden">{requested}</span>
				if route != "" || target != "" {
					<span class="px-1 shrink-0">→</span>
				}
			}
			if route != "" {
				<span class="shrink min-w-3 nowrap truncate overflow-hidden">{route}</span>
				if target != "" {
					<span class="px-1 shrink-0">→</span>
				}
			}
			if target != "" {
				<span class="flex-shrink-5 min-w-5 nowrap truncate overflow-hidden">{target}</span>
			}
		}
		<span class="w-2 grow shrink min-w-0"></span>
		if attempts := activityAttemptEvidence(row); attempts != "" {
			if client := activityClientEvidence(row); showClient && client != "" {
				<span class="w-18 flex-shrink-5 min-w-0 nowrap truncate overflow-hidden">{client}</span>
				<span class="w-2 shrink-0"></span>
			}
			<span class="shrink-0 nowrap">{attempts}</span>
		} else if client := activityClientEvidence(row); showClient && client != "" {
			<span class="w-22 flex-shrink-5 min-w-0 nowrap truncate overflow-hidden">{client}</span>
		}
		<span class="w-2 shrink-0"></span>
		<span class="w-3 shrink-0 nowrap truncate overflow-hidden">{activityStatusLabel(row)}</span>
		<span class="w-1 shrink-0"></span>
		<span class="w-7 shrink-0 nowrap truncate overflow-hidden">{activityDurationLabel(row)}</span>
		<span class="w-3 shrink-0"></span>
		</div>
	</div>
}

func activityPathParts(row readmodel.ActivityRowReadModel) (string, string, string) {
	requested := strings.TrimSpace(row.RequestedModel) // swobu:io-string source=boundary
	routeID := activityRouteIdentity(row)
	route := activityRouteLabel(row)
	if requested != "" && requested == routeID {
		requested = ""
	}
	return requested, route, activityTarget(row)
}

templ ActivityColumns(observedAt string, route string, evidence string, status string, duration string) {
	<div class="flex-row flex-nowrap w-full">
		<span class="w-5 shrink-0"></span>
		<div class="flex-row flex-nowrap gap-2 flex-1 min-w-0">
		<span class="w-8 shrink-0 nowrap truncate overflow-hidden">{observedAt}</span>
		<span class="w-18 shrink-0 min-w-0 nowrap truncate overflow-hidden">{route}</span>
		<span class="grow shrink min-w-0 nowrap truncate overflow-hidden">{evidence}</span>
		<span class="w-3 shrink-0 nowrap truncate overflow-hidden">{status}</span>
		<span class="w-7 shrink-0 nowrap truncate overflow-hidden">{duration}</span>
		<span class="w-3 shrink-0"></span>
		</div>
	</div>
}
