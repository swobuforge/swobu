package workspace_overview

import (
	tui "github.com/grindlemire/go-tui"
	workspace_delete "github.com/swobuforge/swobu/internal/cockpit/features/workspace_delete"
	workspace_edit "github.com/swobuforge/swobu/internal/cockpit/features/workspace_edit"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
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
	return SectionWithWorkspaceCommands(model, nil)
}

func SectionWithWorkspaceCommands(model readmodel.WorkspaceReadModel, commands ports.WorkspaceCommands) *SectionView {
	section := &SectionView{
		Model:               model,
		Expanded:            tui.NewState(true),
		SummaryOnly:         tui.NewState(false),
		CopiedClientBaseURL: tui.NewState(false),
		OpenRun:            tui.NewState(readmodel.RunCommandID("")),
	}
	section.WorkspaceEdit = workspace_edit.NewWorkflow(model, saveWorkspace(commands), section.workspaceSaved)
	section.DeleteConfirmation = workspace_delete.Confirmation(model, deleteWorkspace(commands), section.workspaceDeleted)
	return section
}

func saveWorkspace(commands ports.WorkspaceCommands) workspace_edit.SaveFunc {
	if commands == nil {
		return nil
	}
	return commands.SaveWorkspace
}

func deleteWorkspace(commands ports.WorkspaceCommands) workspace_delete.DeleteFunc {
	if commands == nil {
		return nil
	}
	return commands.DeleteWorkspace
}

func (s *SectionView) workspaceSaved(workspace readmodel.WorkspaceReadModel) {
	s.Model = workspace
	s.DeleteConfirmation.Workspace = workspace
}

func (s *SectionView) workspaceDeleted(workspaceID readmodel.WorkspaceID) {
	if workspaceID == s.Model.ID {
		s.SummaryOnly.Set(true)
	}
}

func (s *SectionView) copyClientBaseURL() {
	s.CopiedClientBaseURL.Set(true)
}

func (s *SectionView) openRun(command readmodel.RunCommandReadModel) {
	s.OpenRun.Set(command.ID)
}

func (s *SectionView) Back() bool {
	if s.DeleteConfirmation.Back() {
		return true
	}
	if s.WorkspaceEdit.Back() {
		return true
	}
	return false
}

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		@SectionHeader("workspace", s.Expanded.Get())
		if s.Expanded.Get() {
			@s.WorkspaceEdit
			if s.Model.IsDraft() {
				@InertRow("client base URL", s.WorkspaceEdit.ClientBaseURLPreview(), "")
			} else {
				@FocusableRow("client base URL", s.Model.ClientBaseURL, "copy ↵", s.copyClientBaseURL)
				if !s.SummaryOnly.Get() {
					if len(s.Model.RunCommands) > 0 {
						@FocusableRow("run once", s.Model.RunCommands[0].Label, "open ↵", func() { s.openRun(s.Model.RunCommands[0]) })
					}
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
