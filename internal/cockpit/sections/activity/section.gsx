package activity

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

templ Section(workspace readmodel.WorkspaceReadModel) {
	<div class="flex-col w-full">
		@SectionHeader("activity", workspace.View.ActivityExpanded)
		if workspace.View.ActivityExpanded {
			if latest, ok := workspace.Activity.LatestRow(); ok {
				@ContentRow("latest", latest.RowValue(), activityAction(latest), workspace.View.FocusedActivityID == latest.ID)
				if workspace.View.ExpandedActivityID == latest.ID {
					@DetailRow("resolved", latest.ResolvedName)
					@DetailRow("model", latest.Model)
					for i, attempt := range latest.Attempts {
						@DetailRow(fmt.Sprintf("attempt %d", i+1), attemptLabel(attempt))
					}
					<br />
					@DetailRow("tokens in", commaInt(latest.TokensIn))
					@DetailRow("tokens out", commaInt(latest.TokensOut))
				}
			} else if workspace.IsDraft() {
				@ContentRow("(no activity)", "", "", false)
			} else {
				@ContentRow("latest", "no requests yet", "", false)
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

templ ContentRow(label string, value string, action string, focused bool) {
	<div class="flex-row w-full">
		if focused {
			<span class="w-5">{">"}</span>
		} else {
			<span class="w-5"></span>
		}
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
