package activity

import (
	"context"
	"fmt"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type SectionView struct {
	Workspace        readmodel.WorkspaceReadModel
	Expanded         *tui.State[bool]
	OpenActivity     *tui.State[readmodel.ActivityID]
	// ActivitySnapshot owns the visible activity projection and redraw signal.
	ActivitySnapshot *tui.State[readmodel.ActivityReadModel]
	Ctx              context.Context
	ActivityQuery    ports.ActivityQueries
}

type activityRefreshResult struct {
	workspaceID readmodel.WorkspaceID
	query       ports.ActivityQueries
	activity    readmodel.ActivityReadModel
}

func Section(workspace readmodel.WorkspaceReadModel, ctx context.Context, activityQuery ports.ActivityQueries) *SectionView {
	if ctx == nil {
		ctx = context.Background()
	}
	return &SectionView{
		Workspace:       workspace,
		Expanded:        tui.NewState(true),
		OpenActivity:    tui.NewState(readmodel.ActivityID("")),
		ActivitySnapshot: tui.NewState(workspace.Activity),
		Ctx:             ctx,
		ActivityQuery:   activityQuery,
	}
}

func (s *SectionView) openActivity(row readmodel.ActivityRowReadModel) {
	s.OpenActivity.Set(row.ID)
}

func (s *SectionView) onToggle(expanded bool) {
	if expanded {
		s.Refresh()
	}
}

// Refresh re-queries the latest visible activity snapshot when the section is
// shown again or the cockpit resumes from a foreground return.
func (s *SectionView) Refresh() bool {
	return s.refreshActivity()
}

func (s *SectionView) refreshActivity() bool {
	if !s.Expanded.Get() || s.ActivityQuery == nil || s.Workspace.IsDraft() {
		return false
	}
	activity, err := s.ActivityQuery.ListActivity(s.Ctx, ports.ListActivityRequest{WorkspaceID: s.Workspace.ID, Limit: 5})
	if err != nil {
		return false
	}
	return s.applyActivityRefresh(activityRefreshResult{
		workspaceID: s.Workspace.ID,
		query:       s.ActivityQuery,
		activity:    activity,
	})
}

func (s *SectionView) applyActivityRefresh(result activityRefreshResult) bool {
	if !s.Expanded.Get() || s.Workspace.IsDraft() || s.Workspace.ID != result.workspaceID || s.ActivityQuery != result.query {
		return false
	}
	if activitySurfaceEqual(s.ActivitySnapshot.Get(), result.activity) {
		return false
	}
	s.ActivitySnapshot.Set(result.activity)
	currentOpen := s.OpenActivity.Get()
	latest, ok := result.activity.LatestRow()
	if !ok {
		if currentOpen != "" {
			s.OpenActivity.Set("")
		}
		return true
	}
	if currentOpen != "" && currentOpen != latest.ID {
		s.OpenActivity.Set("")
	}
	return true
}

func activitySurfaceEqual(current readmodel.ActivityReadModel, fresh readmodel.ActivityReadModel) bool {
	currentLatest, currentOK := current.LatestRow()
	freshLatest, freshOK := fresh.LatestRow()
	if currentOK != freshOK {
		return false
	}
	if !currentOK {
		return true
	}
	return activityRowVisibleEqual(currentLatest, freshLatest)
}

func activityRowVisibleEqual(current readmodel.ActivityRowReadModel, fresh readmodel.ActivityRowReadModel) bool {
	if current.ID != fresh.ID ||
		current.ObservedAt != fresh.ObservedAt ||
		current.ClientLabel != fresh.ClientLabel ||
		current.RouteID != fresh.RouteID ||
		current.RouteLabel != fresh.RouteLabel ||
		current.Status != fresh.Status ||
		current.HTTPStatus != fresh.HTTPStatus ||
		current.Duration != fresh.Duration ||
		current.Error != fresh.Error ||
		current.ResolvedName != fresh.ResolvedName ||
		current.Model != fresh.Model ||
		current.TokensIn != fresh.TokensIn ||
		current.TokensOut != fresh.TokensOut ||
		len(current.Attempts) != len(fresh.Attempts) {
		return false
	}
	for i := range current.Attempts {
		if current.Attempts[i] != fresh.Attempts[i] {
			return false
		}
	}
	return true
}

func (s *SectionView) Back() bool {
	if s.OpenActivity.Get() != "" {
		s.OpenActivity.Set("")
		return true
	}
	return false
}

func sectionHeaderKey(s *SectionView) string {
	return "section-header:activity:" + string(s.Workspace.ID)
}

func SectionHeaderComponent(s *SectionView) tui.Component {
	disclosure := ui.NewSectionDisclosure(sectionHeaderKey(s), "activity", s.Expanded)
	disclosure.OnToggle = s.onToggle
	return disclosure
}


// ---------------------------------------------------------------------------
// Selectable row components
// ---------------------------------------------------------------------------

func latestRowKey() string { return "activity:latest" }

func LatestRowComponent(s *SectionView) *ui.SelectableRow {
	latest, ok := s.ActivitySnapshot.Get().LatestRow()
	if !ok {
		return nil
	}
	return ui.NewSelectableRow(
		latestRowKey(),
		"latest",
		latest.RowValue(),
		activityAction(latest),
		func() { s.openActivity(latest) },
	)
}

// ---------------------------------------------------------------------------
// Section render
// ---------------------------------------------------------------------------

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		<div key={sectionHeaderKey(s)} class="w-full">
			@SectionHeaderComponent(s)
		</div>
		if s.Expanded.Get() {
			if latest, ok := s.ActivitySnapshot.Get().LatestRow(); ok {
				<div key={latestRowKey()} class="w-full">
					@LatestRowComponent(s)
				</div>
				if s.OpenActivity.Get() == latest.ID {
					@DetailRow("resolved", latest.ResolvedName, "")
					@DetailRow("model", latest.Model, "")
					for i, attempt := range latest.Attempts {
						@DetailRow(fmt.Sprintf("attempt %d", i+1), attemptLabel(attempt), "")
					}
					if latest.TokensIn > 0 || latest.TokensOut > 0 {
						<br />
					}
					if latest.TokensIn > 0 {
						@DetailRow("tokens in", commaInt(latest.TokensIn), "")
					}
					if latest.TokensOut > 0 {
						@DetailRow("tokens out", commaInt(latest.TokensOut), "")
					}
				}
			} else if s.Workspace.IsDraft() {
				@InertRow("(no activity)", "", "")
			} else {
				@InertRow("latest", "no requests yet", "")
			}
		}
	</div>
}

// ---------------------------------------------------------------------------
// Layout helpers
// ---------------------------------------------------------------------------

templ InertRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-5"></span>
		<span class="w-18">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
	</div>
}

// DetailRow renders nested trace rows with a deeper left margin than the
// top-level activity row so expanded inspection stays visually subordinate.
templ DetailRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-8"></span>
		<span class="w-15">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
	</div>
}

// ---------------------------------------------------------------------------
// Pure display helpers
// ---------------------------------------------------------------------------

func activityAction(row readmodel.ActivityRowReadModel) string {
	if row.Error {
		return "err ↵"
	}
	return ""
}

func attemptLabel(attempt readmodel.ActivityAttemptReadModel) string {
	label := attempt.Label
	if label == "" {
		label = fmt.Sprintf("attempt #%d", attempt.Rank)
	}
	result := ""
	switch attempt.Result {
	case readmodel.ActivityAttemptSucceeded:
		result = "success"
	case readmodel.ActivityAttemptFailed:
		result = "failed"
	case readmodel.ActivityAttemptSkipped:
		result = "skipped"
	}
	if result != "" {
		return fmt.Sprintf("%s rank %d — %s", label, attempt.Rank, result)
	}
	return fmt.Sprintf("%s rank %d", label, attempt.Rank)
}

func commaInt(n int) string {
	s := fmt.Sprint(n)
	var result strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteRune(r)
	}
	return result.String()
}
