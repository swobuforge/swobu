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
	SaveWorkspace            workspace_edit.SaveFunc
	DeleteWorkspace          workspace_delete.DeleteFunc
	OnWorkspaceSaved         func(readmodel.WorkspaceReadModel)
	OnWorkspaceDeleted       func(readmodel.WorkspaceID)
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
		section.SaveWorkspace = commands[0].SaveWorkspace
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
					@InertRow("endpoint", WorkspaceEdit(s).ClientBaseURLPreview(), "")
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

templ InertRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span class="w-18">{label}</span>
		<span class="w-32">{value}</span>
		<span>{action}</span>
	</div>
}
