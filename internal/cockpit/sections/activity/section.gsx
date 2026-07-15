package activity

import (
	"fmt"

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

func FocusableRow(label string, value string, action string, activate func()) *ui.FocusableRowView {
	return ui.NewFocusableRowView(label, value, action, activate)
}

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		@SectionHeader("activity", s.Expanded.Get())
		if s.Expanded.Get() {
			if latest, ok := s.Workspace.Activity.LatestRow(); ok {
				if app != nil {
					@FocusableRow("latest", latest.RowValue(), activityAction(latest), func() { s.openActivity(latest) })
				} else {
					@FocusablePreviewRow("latest", latest.RowValue(), activityAction(latest), func() { s.openActivity(latest) })
				}
				if s.OpenActivity.Get() == latest.ID {
					@DetailRow("resolved", latest.ResolvedName)
					@DetailRow("model", latest.Model)
					for i, attempt := range latest.Attempts {
						@DetailRow(fmt.Sprintf("attempt %d", i+1), attemptLabel(attempt))
					}
					if latest.TokensIn > 0 || latest.TokensOut > 0 {
						<br />
					}
					if latest.TokensIn > 0 {
						@DetailRow("tokens in", commaInt(latest.TokensIn))
					}
					if latest.TokensOut > 0 {
						@DetailRow("tokens out", commaInt(latest.TokensOut))
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

templ FocusablePreviewRow(label string, value string, action string, activate func()) {
	<div class="flex-row w-full" onActivate={activate}>
		<span class="w-5"></span>
		<span class="w-18">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
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

templ DetailRow(label string, value string) {
	<div class="flex-row w-full">
		<span class="w-8"></span>
		<span class="w-15">{label}</span>
		<span>{value}</span>
	</div>
}


func activityAction(row readmodel.ActivityRowReadModel) string {
	if row.Error {
		return "err ↵"
	}
	return ""
}

func attemptLabel(attempt readmodel.ActivityAttemptReadModel) string {
	return fmt.Sprintf("%s rank %d — %s", attempt.Label, attempt.Rank, attemptResultLabel(attempt.Result))
}

func attemptResultLabel(result readmodel.ActivityAttemptResult) string {
	switch result {
	case readmodel.ActivityAttemptFailed:
		return "failed"
	case readmodel.ActivityAttemptSkipped:
		return "skipped"
	default:
		return "success"
	}
}

func commaInt(n int) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("%d,%03d", n/1000, n%1000)
}
