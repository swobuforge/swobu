package workspace_overview

import (
	tui "github.com/grindlemire/go-tui"
	workspace_delete "github.com/swobuforge/swobu/internal/cockpit/features/workspace_delete"
	workspace_edit "github.com/swobuforge/swobu/internal/cockpit/features/workspace_edit"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

type SectionView struct {
	Model               readmodel.WorkspaceReadModel
	Expanded            *tui.State[bool]
	SummaryOnly         *tui.State[bool]
	CopiedClientBaseURL *tui.State[bool]
	OpenRun            *tui.State[readmodel.RunCommandID]
	WorkspaceEdit      *workspace_edit.Workflow
	DeleteConfirmation *workspace_delete.ConfirmationView
}

func Section(model readmodel.WorkspaceReadModel) *SectionView {
	return &SectionView{
		Model:               model,
		Expanded:            tui.NewState(true),
		SummaryOnly:         tui.NewState(false),
		CopiedClientBaseURL: tui.NewState(false),
		OpenRun:            tui.NewState(readmodel.RunCommandID("")),
		WorkspaceEdit:      workspace_edit.NewWorkflow(),
		DeleteConfirmation: workspace_delete.Confirmation(model),
	}
}

func (s *SectionView) copyClientBaseURL() {
	s.CopiedClientBaseURL.Set(true)
}

func (s *SectionView) openRun(command readmodel.RunCommandReadModel) {
	s.OpenRun.Set(command.ID)
}

func (s *SectionView) openWorkspaceEdit() {
	s.WorkspaceEdit.OpenEditor()
}

func (s *SectionView) Back() bool {
	if s.DeleteConfirmation.Back() {
		return true
	}
	if s.WorkspaceEdit.Open.Get() {
		s.WorkspaceEdit.Open.Set(false)
		return true
	}
	return false
}

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		@SectionHeader("workspace", s.Expanded.Get())
		if s.Expanded.Get() {
			if s.Model.IsDraft() {
				@FocusableRow("slug", "", "create ↵", func() {})
				@InertRow("client base URL", "(derived from slug)", "")
			} else {
				@FocusableRow("client base URL", s.Model.ClientBaseURL, "copy ↵", s.copyClientBaseURL)
				if !s.SummaryOnly.Get() {
					if len(s.Model.RunCommands) > 0 {
						@FocusableRow("run once", s.Model.RunCommands[0].Label, "open ↵", func() { s.openRun(s.Model.RunCommands[0]) })
					}
					@FocusableRow("edit workspace", "", "open ↵", s.openWorkspaceEdit)
					@s.DeleteConfirmation
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
