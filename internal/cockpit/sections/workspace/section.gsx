package workspace

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

type SectionView struct {
	Model               readmodel.WorkspaceReadModel
	CopiedClientBaseURL *tui.State[bool]
	OpenRun            *tui.State[readmodel.RunCommandID]
	OpenWorkspaceEdit  *tui.State[bool]
}

func Section(model readmodel.WorkspaceReadModel) *SectionView {
	return &SectionView{
		Model:               model,
		CopiedClientBaseURL: tui.NewState(false),
		OpenRun:            tui.NewState(readmodel.RunCommandID("")),
		OpenWorkspaceEdit:  tui.NewState(false),
	}
}

func (s *SectionView) copyClientBaseURL() {
	s.CopiedClientBaseURL.Set(true)
}

func (s *SectionView) openRun(command readmodel.RunCommandReadModel) {
	s.OpenRun.Set(command.ID)
}

func (s *SectionView) openWorkspaceEdit() {
	s.OpenWorkspaceEdit.Set(true)
}

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		@SectionHeader("workspace", s.Model.View.WorkspaceExpanded)
		if s.Model.View.WorkspaceExpanded {
			if s.Model.IsDraft() {
				@FocusableRow("slug", "", "create ↵", func() {})
				@InertRow("client base URL", "(derived from slug)", "")
			} else {
				@FocusableRow("client base URL", s.Model.ClientBaseURL, "copy ↵", s.copyClientBaseURL)
				if !s.Model.View.WorkspaceSummaryOnly {
					if len(s.Model.RunCommands) > 0 {
						@FocusableRow("run once", s.Model.RunCommands[0].Label, "open ↵", func() { s.openRun(s.Model.RunCommands[0]) })
					}
					@FocusableRow("edit workspace", "", "open ↵", s.openWorkspaceEdit)
					if s.Model.View.DeleteWorkspaceConfirm {
						@InertRow("delete workspace "+confirmationSlug(s.Model)+"?", "", "y/n")
					}
				}
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

templ FocusableRow(label string, value string, action string, activate func()) {
	<div class="flex-row w-full focusable" onActivate={activate}>
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

func confirmationSlug(model readmodel.WorkspaceReadModel) string {
	if model.View.WorkspaceConfirmationID != "" {
		return string(model.View.WorkspaceConfirmationID)
	}
	return model.Slug
}
