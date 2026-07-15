package workspace_overview

import (
	tui "github.com/grindlemire/go-tui"
	workspace_delete "github.com/swobuforge/swobu/internal/cockpit/features/workspace_delete"
	workspace_edit "github.com/swobuforge/swobu/internal/cockpit/features/workspace_edit"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type SectionView struct {
	Model               readmodel.WorkspaceReadModel
	Expanded            *tui.State[bool]
	SummaryOnly         *tui.State[bool]
	CopiedClientBaseURL *tui.State[bool]
	OpenRun            *tui.State[readmodel.RunCommandID]
	SaveWorkspace      workspace_edit.SaveFunc
	DeleteWorkspace    workspace_delete.DeleteFunc
	OnWorkspaceSaved   func(readmodel.WorkspaceReadModel)
	OnWorkspaceDeleted func(readmodel.WorkspaceID)
	InitialDeleteID    readmodel.WorkspaceID
}

func Section(model readmodel.WorkspaceReadModel, commands ...ports.WorkspaceCommands) *SectionView {
	section := &SectionView{
		Model:               model,
		Expanded:            tui.NewState(true),
		SummaryOnly:         tui.NewState(false),
		CopiedClientBaseURL: tui.NewState(false),
		OpenRun:            tui.NewState(readmodel.RunCommandID("")),
	}
	if len(commands) > 0 && commands[0] != nil {
		section.SaveWorkspace = commands[0].SaveWorkspace
		section.DeleteWorkspace = commands[0].DeleteWorkspace
	}
	return section
}

func (s *SectionView) workspaceSaved(workspace readmodel.WorkspaceReadModel) {
	s.Model = workspace
	if s.OnWorkspaceSaved != nil {
		s.OnWorkspaceSaved(workspace)
	}
}

func (s *SectionView) workspaceDeleted(workspaceID readmodel.WorkspaceID) {
	if workspaceID == s.Model.ID {
		s.SummaryOnly.Set(true)
	}
	if s.OnWorkspaceDeleted != nil {
		s.OnWorkspaceDeleted(workspaceID)
	}
}

func (s *SectionView) copyClientBaseURL() {
	s.CopiedClientBaseURL.Set(true)
}

func (s *SectionView) openRun(command readmodel.RunCommandReadModel) {
	s.OpenRun.Set(command.ID)
}

func (s *SectionView) OpenDeleteConfirmation(workspaceID readmodel.WorkspaceID) {
	s.InitialDeleteID = workspaceID
}

func WorkspaceEdit(s *SectionView) *workspace_edit.Workflow {
	return workspace_edit.NewWorkflow(
		s.Model,
		s.SaveWorkspace,
		s.workspaceSaved,
	)
}

func DeleteConfirmation(s *SectionView) *workspace_delete.ConfirmationView {
	confirmation := workspace_delete.Confirmation(
		s.Model,
		s.DeleteWorkspace,
		s.workspaceDeleted,
	)
	if s.InitialDeleteID != "" {
		confirmation.Request(s.InitialDeleteID)
	}
	return confirmation
}

func FocusableRow(label string, value string, action string, activate func()) *ui.FocusableRowView {
	return ui.NewFocusableRowView(label, value, action, activate)
}

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		@SectionHeader("workspace", s.Expanded.Get())
		if s.Expanded.Get() {
			if app != nil {
				<div key={workspaceEditKey(s)} class="w-full">
					@WorkspaceEdit(s)
				</div>
			} else {
				@WorkspaceEditPreview(WorkspaceEdit(s))
			}
			if s.Model.IsDraft() {
				@InertRow("client base URL", WorkspaceEdit(s).ClientBaseURLPreview(), "")
			} else {
				if app != nil {
					@FocusableRow("client base URL", s.Model.ClientBaseURL, "copy ↵", s.copyClientBaseURL)
				} else {
					@FocusablePreviewRow("client base URL", s.Model.ClientBaseURL, "copy ↵", s.copyClientBaseURL)
				}
				if !s.SummaryOnly.Get() {
					if len(s.Model.RunCommands) > 0 {
						if app != nil {
							@FocusableRow("run once", s.Model.RunCommands[0].Label, "open ↵", func() { s.openRun(s.Model.RunCommands[0]) })
						} else {
							@FocusablePreviewRow("run once", s.Model.RunCommands[0].Label, "open ↵", func() { s.openRun(s.Model.RunCommands[0]) })
						}
					}
					if app != nil {
						<div key={workspaceDeleteKey(s)} class="w-full">
							@DeleteConfirmation(s)
						</div>
					} else {
						@DeleteConfirmationPreview(DeleteConfirmation(s))
					}
				}
			}
		}
	</div>
}

templ WorkspaceEditPreview(workflow *workspace_edit.Workflow) {
	<div class="flex-col w-full">
		<div class="flex-row w-full" onActivate={workflow.Activate}>
			<span class="w-5"></span>
			<span class="w-18">slug</span>
			<span class="w-30">{workflow.ValueLabel()}</span>
			<span>{workflow.ActionLabel()}</span>
		</div>
		if workflow.ErrorMessage() != "" {
			<div class="flex-row w-full">
				<span class="w-9"></span>
				<span>{workflow.ErrorMessage()}</span>
			</div>
		}
	</div>
}

templ DeleteConfirmationPreview(confirmation *workspace_delete.ConfirmationView) {
	<div class="flex-col w-full">
		@FocusablePreviewRow("delete", confirmation.RowValue(), confirmation.ActionLabel(), confirmation.Activate)
	</div>
}

func workspaceEditKey(s *SectionView) string {
	return "workspace-edit:" + workspaceIdentity(s)
}

func workspaceDeleteKey(s *SectionView) string {
	return "workspace-delete:" + workspaceIdentity(s)
}

func workspaceIdentity(s *SectionView) string {
	if s.Model.ID != "" {
		return string(s.Model.ID)
	}
	if s.Model.Slug != "" {
		return s.Model.Slug
	}
	return "+"
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
