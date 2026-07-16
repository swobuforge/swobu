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

func SectionHeaderComponent(s *SectionView) tui.Component {
	if s.Model.IsDraft() {
		return ui.NewTextComponent("  new workspace")
	}
	return ui.NewSectionDisclosure(sectionHeaderKey(s), "workspace", s.Expanded)
}

// ---------------------------------------------------------------------------
// Endpoint row
// ---------------------------------------------------------------------------

func endpointRowKey(s *SectionView) string { return "endpoint:" + workspaceIdentity(s) }

func endpointAction(s *SectionView) string {
	if s.CopiedEndpoint.Get() {
		return "copied"
	}
	return "copy ↵"
}

// EndpointRowComponent mounts the hero endpoint row: two visual lines
// (compatibility badges + URL) as a single focusable component.
func EndpointRowComponent(s *SectionView) tui.Component {
	row := &endpointRowView{s: s}
	row.SelectBase = ui.NewSelectBase(endpointRowKey(s))
	return row
}

type endpointRowView struct {
	ui.SelectBase
	s *SectionView
}

func (r *endpointRowView) BindApp(app *tui.App) {
	r.SelectBase.BindApp(app)
}

func (r *endpointRowView) UnbindApp() {
	r.SelectBase.UnbindApp()
}

func (r *endpointRowView) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*endpointRowView)
	if !ok {
		return
	}
	r.s = f.s
}

func (r *endpointRowView) Render(app *tui.App) *tui.Element {
	_ = app
	s := r.s

	// Build both content rows as a single flex column so they share the same
	// full width even inside a margin-shifted container.
	col := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)

	row1 := ui.ActionRow(r.Arrow(), "endpoint", s.Model.ClientBaseURL, endpointAction(s),
		tui.WithFocusable(true),
		tui.WithOnFocus(r.OnFocus),
		tui.WithOnBlur(r.OnBlur),
		tui.WithOnActivate(func() { s.copyEndpoint() }),
	)
	col.AddChild(row1)

	row2 := ui.ActionRow("", "", s.Model.CompatibleClients, "")
	col.AddChild(row2)

	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)
	root.AddChild(col)

	if r.Ref != nil {
		// The focusable element is row1 inside the wrapper; point Ref there
		// so Arrow() and IsFocused() reflect the actual focus state.
		r.Ref.Set(row1)
	}
	return root
}

func (r *endpointRowView) KeyMap() tui.KeyMap {
	return r.WithTraversal(ui.ActivateFocused(func(tui.KeyEvent) {
		r.s.copyEndpoint()
	}))
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
			<div class="ml-3 w-full">
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

templ InertRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-2"></span>
		<span class="w-18">{label}</span>
		<span class="w-32">{value}</span>
		<span>{action}</span>
	</div>
}
