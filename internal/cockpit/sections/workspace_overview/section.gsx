package workspace_overview

import (
	tui "github.com/grindlemire/go-tui"
	workspace_delete "github.com/swobuforge/swobu/internal/cockpit/features/workspace_delete"
	workspace_edit "github.com/swobuforge/swobu/internal/cockpit/features/workspace_edit"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// ---------------------------------------------------------------------------
// Section state
// ---------------------------------------------------------------------------

type SectionView struct {
	Model                    readmodel.WorkspaceReadModel
	Expanded                 *tui.State[bool]
	CopiedEndpoint           *tui.State[bool]
	RenameWorkspace          workspace_edit.RenameFunc
	DeleteWorkspace          workspace_delete.DeleteFunc
	OnWorkspaceSaved         func(readmodel.WorkspaceReadModel)
	OnWorkspaceDeleted       func(readmodel.WorkspaceID)
	OnWorkspaceDiscarded     func()
	// PendingDeleteWorkspaceID seeds the delete confirmation child while the
	// delete row is armed. The parent keeps the request here so Back() can clear
	// it without holding a persistent child reference.
	PendingDeleteWorkspaceID *tui.State[readmodel.WorkspaceID]
}

func Section(model readmodel.WorkspaceReadModel, commands ...ports.WorkspaceCommands) *SectionView {
	section := &SectionView{
		Model:                    model,
		Expanded:                 tui.NewState(true),
		CopiedEndpoint:           tui.NewState(false),
		PendingDeleteWorkspaceID: tui.NewState(readmodel.WorkspaceID("")),
	}
	if len(commands) > 0 && commands[0] != nil {
		section.RenameWorkspace = commands[0].RenameWorkspace
		section.DeleteWorkspace = commands[0].DeleteWorkspace
	}
	return section
}

// ---------------------------------------------------------------------------
// Lifecycle callbacks
// ---------------------------------------------------------------------------

func (s *SectionView) workspaceSaved(workspace readmodel.WorkspaceReadModel) {
	s.Model = workspace
	s.resetTransientState()
	if s.OnWorkspaceSaved != nil {
		s.OnWorkspaceSaved(workspace)
	}
}

func (s *SectionView) workspaceDeleted(workspaceID readmodel.WorkspaceID) {
	if workspaceID == s.Model.ID {
		// Summary-only rendering keeps the section visible but suppresses
		// destructive actions after the workspace it owned is gone.
	}
	if s.OnWorkspaceDeleted != nil {
		s.OnWorkspaceDeleted(workspaceID)
	}
}

func (s *SectionView) workspaceDiscarded() error {
	if s.OnWorkspaceDiscarded != nil {
		s.OnWorkspaceDiscarded()
	}
	return nil
}

func (s *SectionView) copyEndpoint() {
	s.CopiedEndpoint.Set(true)
}

func (s *SectionView) resetTransientState() {
	s.CopiedEndpoint.Set(false)
	s.PendingDeleteWorkspaceID.Set("")
}

// ---------------------------------------------------------------------------
// Back / navigation
// ---------------------------------------------------------------------------

func (s *SectionView) Back() bool {
	if s.deleteIsOpen() {
		s.closeDelete()
		return true
	}
	return false
}

func (s *SectionView) deleteIsOpen() bool { return s.PendingDeleteWorkspaceID.Get() != "" }
func (s *SectionView) closeDelete()      { s.PendingDeleteWorkspaceID.Set("") }

func (s *SectionView) OpenDeleteConfirmation(workspaceID readmodel.WorkspaceID) {
	s.PendingDeleteWorkspaceID.Set(workspaceID)
}

// ---------------------------------------------------------------------------
// Feature components
// ---------------------------------------------------------------------------

func WorkspaceEdit(s *SectionView) *workspace_edit.Workflow {
	return workspace_edit.NewWorkflow(
		s.Model,
		s.RenameWorkspace,
		s.workspaceSaved,
	)
}

func DeleteConfirmation(s *SectionView) *workspace_delete.ConfirmationView {
	confirmation := workspace_delete.Confirmation(
		s.Model,
		s.DeleteWorkspace,
		s.workspaceDeleted,
	)
	if s.PendingDeleteWorkspaceID.Get() != "" {
		confirmation.Request(s.PendingDeleteWorkspaceID.Get())
	}
	confirmation.OnArm = func(_ readmodel.WorkspaceID) {
		s.OpenDeleteConfirmation(s.Model.ID)
	}
	confirmation.OnCancel = func(_ readmodel.WorkspaceID) {
		s.closeDelete()
	}
	return confirmation
}

func DraftDiscardComponent(s *SectionView) *ui.ConfirmActionRow {
	copy := ui.ConfirmActionCopy{
		Label: "discard", IdleValue: "setup", IdleAction: "discard ↵",
		ConfirmValue: "discard " + s.Model.Slug + "?", ConfirmAction: "confirm ↵",
		SubmittingValue: "discarding setup…", SubmittingHint: "wait",
		FailedValue: "discard failed", FailedAction: "retry ↵",
	}
	return ui.NewConfirmActionRow("workspace-discard:+", copy, s.workspaceDiscarded)
}

// ---------------------------------------------------------------------------
// Mount keys
// ---------------------------------------------------------------------------

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

func sectionHeaderKey(s *SectionView) string { return "section-header:" + workspaceIdentity(s) }

func WorkspaceDisclosureComponent(s *SectionView) tui.Component {
	return ui.NewSectionDisclosure(sectionHeaderKey(s), "workspace", s.Expanded)
}

// ---------------------------------------------------------------------------
// Section render
// ---------------------------------------------------------------------------

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		if s.Model.IsDraft() {
			@DraftWorkspaceHeader()
		} else {
			<div key={sectionHeaderKey(s)} class="w-full">
				@WorkspaceDisclosureComponent(s)
			</div>
		}
		if s.Expanded.Get() {
			<div class="pl-3 w-full">
				if s.Model.IsDraft() {
					<div key={workspaceEditKey(s)} class="w-full">
						@WorkspaceEdit(s)
					</div>
					if s.Model.Slug != "" {
						<div key={"workspace-discard:+"} class="w-full">
							@DraftDiscardComponent(s)
						</div>
					}
				} else {
					<div key={endpointRowKey(s)} class="w-full">
						@EndpointRowComponent(s)
					</div>
					<div key={workspaceEditKey(s)} class="w-full">
						@WorkspaceEdit(s)
					</div>
					<div key={workspaceDeleteKey(s)} class="w-full">
						@DeleteConfirmation(s)
					</div>
				}
			</div>
		}
	</div>
}

// ---------------------------------------------------------------------------
// Layout helpers
// ---------------------------------------------------------------------------

templ DraftWorkspaceHeader() {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span>new workspace</span>
	</div>
}
