package activity

import (
	"fmt"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type SectionView struct {
	Workspace    readmodel.WorkspaceReadModel
	Expanded     *tui.State[bool]
	OpenActivity *tui.State[readmodel.ActivityID]
}

func Section(workspace readmodel.WorkspaceReadModel) *SectionView {
	return &SectionView{
		Workspace:    workspace,
		Expanded:     tui.NewState(true),
		OpenActivity: tui.NewState(readmodel.ActivityID("")),
	}
}

func (s *SectionView) openActivity(row readmodel.ActivityRowReadModel) {
	s.OpenActivity.Set(row.ID)
}

func (s *SectionView) Back() bool {
	if s.OpenActivity.Get() != "" {
		s.OpenActivity.Set("")
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Selectable row components
// ---------------------------------------------------------------------------

func latestRowKey() string { return "activity:latest" }

func LatestRowComponent(s *SectionView) *ui.SelectableRow {
	latest, ok := s.Workspace.Activity.LatestRow()
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
		@SectionHeader("activity", s.Expanded.Get())
		if s.Expanded.Get() {
			if latest, ok := s.Workspace.Activity.LatestRow(); ok {
				if app != nil {
					<div key={latestRowKey()} class="w-full">
						@LatestRowComponent(s)
					</div>
				} else {
					@InertRow("latest", latest.RowValue(), activityAction(latest))
				}
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

templ SectionHeader(label string, expanded bool) {
	<div class="flex-row">
		<span class="w-2"></span>
		if expanded {
			<span>{label + " ▾"}</span>
		} else {
			<span>{label + " ▸"}</span>
		}
	</div>
}

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
