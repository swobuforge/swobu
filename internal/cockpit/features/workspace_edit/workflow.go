package workspace_edit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// SaveFunc is the narrow command boundary for create and edit submissions.
type SaveFunc func(context.Context, ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error)

// Mode describes whether the shared workflow is creating or editing.
type Mode int

const (
	ModeEdit Mode = iota
	ModeCreate
)

// Phase is the workspace slug row lifecycle.
type Phase int

const (
	PhaseViewing Phase = iota
	PhaseEditing
	PhaseSubmitting
	PhaseFailed
)

// Workflow owns the workspace slug lifecycle row.
//
// The shared EditableRow component owns the focus shell, cursor, and inline
// text editing. Workflow owns the higher-level create/edit lifecycle, submit
// seam, and validation projection that turns slug state into RFC grammar.
type Workflow struct {
	Workspace   readmodel.WorkspaceReadModel
	Phase       *tui.State[Phase]
	Mode        *tui.State[Mode]
	WorkspaceID *tui.State[readmodel.WorkspaceID]
	Slug        *tui.State[string]
	Error       *tui.State[string]
	Save        SaveFunc
	OnSaved     func(readmodel.WorkspaceReadModel)

	row *ui.EditableRow
}

func workflowKey(workspace readmodel.WorkspaceReadModel) string {
	if workspace.ID != "" {
		return "workspace-edit:" + string(workspace.ID)
	}
	if workspace.Slug != "" {
		return "workspace-edit:" + workspace.Slug
	}
	return "workspace-edit:+"
}

func NewWorkflow(workspace readmodel.WorkspaceReadModel, save SaveFunc, onSaved func(readmodel.WorkspaceReadModel)) *Workflow {
	w := &Workflow{
		Workspace:   workspace,
		Phase:       tui.NewState(PhaseViewing),
		Mode:        tui.NewState(ModeEdit),
		WorkspaceID: tui.NewState(workspace.ID),
		Slug:        tui.NewState(workspace.Slug),
		Error:       tui.NewState(""),
		Save:        save,
		OnSaved:     onSaved,
	}
	w.row = ui.NewEditableRow(workflowKey(workspace), "slug", w.Slug)
	w.row.ValueWidth = 32
	w.row.OnActivate = func() { w.Activate() }
	w.row.OnSubmit = func(_ string) { w.Submit(context.Background()) }
	w.row.OnClose = func() { w.cancelFromRow() }
	w.seedFromWorkspace(workspace)
	w.syncRow()
	return w
}

func (w *Workflow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*Workflow)
	if !ok {
		return
	}
	reseed := w.Workspace.ID != f.Workspace.ID || w.Workspace.State != f.Workspace.State
	w.Workspace = f.Workspace
	w.Save = f.Save
	w.OnSaved = f.OnSaved
	if reseed {
		w.seedFromWorkspace(f.Workspace)
	}
	w.syncRow()
}

func (w *Workflow) OpenEditor(workspace readmodel.WorkspaceReadModel) {
	w.seedFromWorkspace(workspace)
	w.Mode.Set(ModeEdit)
	w.Phase.Set(PhaseEditing)
	w.row.Open()
	w.syncRow()
}

func (w *Workflow) OpenCreate() {
	w.seedFromWorkspace(readmodel.WorkspaceReadModel{State: readmodel.WorkspaceDraft})
	w.row.Open()
	w.syncRow()
}

func (w *Workflow) BindApp(app *tui.App) {
	w.Phase.BindApp(app)
	w.Mode.BindApp(app)
	w.WorkspaceID.BindApp(app)
	w.Slug.BindApp(app)
	w.Error.BindApp(app)
	w.row.BindApp(app)
}

func (w *Workflow) UnbindApp() {
	w.row.UnbindApp()
}

func (w *Workflow) Watchers() []tui.Watcher {
	return w.row.Watchers()
}

func (w *Workflow) Back() bool {
	if !w.IsEditing() {
		return false
	}
	w.row.Cancel()
	return true
}

func (w *Workflow) KeyMap() tui.KeyMap {
	return w.row.KeyMap()
}

// RowComponent syncs the child row props before render and returns the shared
// input row component for templ mounting.
func RowComponent(w *Workflow) tui.Component {
	w.syncRow()
	return w.row
}

func (w *Workflow) seedFromWorkspace(workspace readmodel.WorkspaceReadModel) {
	w.Workspace = workspace
	w.WorkspaceID.Set(workspace.ID)
	w.Slug.Set(workspace.Slug)
	w.Error.Set("")
	if workspace.IsDraft() {
		w.Mode.Set(ModeCreate)
		w.Phase.Set(PhaseEditing)
		w.row.Open()
		return
	}
	w.Mode.Set(ModeEdit)
	w.Phase.Set(PhaseViewing)
	w.row.Close()
}

func (w *Workflow) cancelFromRow() {
	w.Error.Set("")
	w.Slug.Set(w.Workspace.Slug)
	w.Phase.Set(PhaseViewing)
	w.syncRow()
}

func (w *Workflow) closeEdit() {
	w.Error.Set("")
	w.Phase.Set(PhaseViewing)
	w.row.Close()
}

func (w *Workflow) IsEditing() bool {
	return w.Phase.Get() == PhaseEditing || w.Phase.Get() == PhaseFailed || w.Phase.Get() == PhaseSubmitting
}

func (w *Workflow) Arrow() string {
	return w.row.Arrow()
}

func (w *Workflow) Activate() {
	if !w.IsEditing() {
		w.Phase.Set(PhaseEditing)
		w.row.Open()
		w.syncRow()
		return
	}
	if w.ErrorMessage() != "" {
		return
	}
	slug := strings.TrimSpace(w.Slug.Get())
	if w.Mode.Get() == ModeEdit && slug == w.Workspace.Slug {
		w.closeEdit()
		return
	}
	w.Submit(context.Background())
}

func (w *Workflow) Submit(ctx context.Context) {
	slug, err := NormalizeSlug(w.Slug.Get())
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
		w.syncRow()
		return
	}
	if w.Save == nil {
		w.Error.Set("workspace save is not wired yet")
		w.Phase.Set(PhaseFailed)
		w.syncRow()
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseSubmitting)
	workspace, err := w.Save(ctx, ports.SaveWorkspaceRequest{ID: w.WorkspaceID.Get(), Slug: slug})
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
		w.syncRow()
		return
	}
	if w.OnSaved != nil {
		w.OnSaved(workspace)
	}
	w.Workspace = workspace
	w.WorkspaceID.Set(workspace.ID)
	w.Slug.Set(workspace.Slug)
	w.Mode.Set(ModeEdit)
	w.closeEdit()
	w.syncRow()
}

func (w *Workflow) SubmitSlug(value string) {
	w.Slug.Set(value)
	w.Submit(context.Background())
}

func (w *Workflow) ActionLabel() string {
	if msg := strings.TrimSpace(w.ErrorMessage()); msg != "" {
		if msg == "enter a workspace slug" {
			return "required"
		}
		if isDuplicateError(msg) {
			return "duplicate"
		}
		return "invalid"
	}
	if w.Mode.Get() == ModeCreate {
		return "create ↵"
	}
	if w.IsEditing() {
		return "save ↵"
	}
	return "edit ↵"
}

func (w *Workflow) ValueLabel() string {
	if slug := strings.TrimSpace(w.Slug.Get()); slug != "" {
		return w.Slug.Get()
	}
	return ""
}

func (w *Workflow) ErrorMessage() string {
	if msg := strings.TrimSpace(w.Error.Get()); msg != "" {
		return msg
	}
	slug := strings.TrimSpace(w.Slug.Get())
	if slug == "" {
		if w.Mode.Get() == ModeCreate {
			return "enter a workspace slug"
		}
		return ""
	}
	if _, err := NormalizeSlug(slug); err != nil {
		return err.Error()
	}
	return ""
}

func (w *Workflow) visibleError() string {
	return w.ErrorMessage()
}

func (w *Workflow) ClientBaseURLPreview() string {
	if msg := strings.TrimSpace(w.ErrorMessage()); msg != "" {
		return "after create"
	}
	slug, err := NormalizeSlug(w.Slug.Get())
	if err != nil {
		return "after create"
	}
	baseURL := w.Workspace.ClientBaseURL
	if baseURL == "" {
		return "http://127.0.0.1:7926/c/" + slug
	}
	marker := "/c/"
	if i := strings.LastIndex(baseURL, marker); i >= 0 {
		return baseURL[:i+len(marker)] + slug
	}
	return baseURL
}

func (w *Workflow) syncRow() {
	if w.row == nil {
		return
	}
	w.row.Label = "slug"
	w.row.Value = w.Slug
	w.row.ValueWidth = 32
	w.row.Validation = w.rowValidation()
	w.row.ValidationText = w.ErrorMessage()
	if w.Mode.Get() == ModeCreate {
		w.row.ViewAction = "create ↵"
		w.row.EditAction = "create ↵"
		return
	}
	w.row.ViewAction = "edit ↵"
	w.row.EditAction = "save ↵"
}

func (w *Workflow) rowValidation() ui.EditableRowValidation {
	msg := strings.TrimSpace(w.ErrorMessage())
	if msg == "" {
		return ui.EditableRowValidationNone
	}
	if msg == "enter a workspace slug" {
		return ui.EditableRowValidationRequired
	}
	if isDuplicateError(msg) {
		return ui.EditableRowValidationDuplicate
	}
	return ui.EditableRowValidationInvalid
}

func isDuplicateError(msg string) bool {
	lowered := strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(lowered, "conflict") ||
		strings.Contains(lowered, "duplicate") ||
		strings.Contains(lowered, "already exists") ||
		strings.Contains(lowered, "taken")
}

func NormalizeSlug(raw string) (string, error) {
	slug := strings.TrimSpace(raw)
	if slug == "" {
		return "", errors.New("enter a workspace slug")
	}
	for _, r := range slug {
		if unicode.IsLower(r) || unicode.IsDigit(r) || r == '-' {
			continue
		}
		return "", fmt.Errorf("use lowercase letters, numbers, and hyphens")
	}
	if slug[0] == '-' || slug[len(slug)-1] == '-' {
		return "", errors.New("slug cannot start or end with hyphen")
	}
	if strings.Contains(slug, "--") {
		return "", errors.New("slug cannot contain consecutive hyphens")
	}
	return slug, nil
}

var (
	_ tui.Component       = (*Workflow)(nil)
	_ tui.KeyListener     = (*Workflow)(nil)
	_ tui.PropsUpdater    = (*Workflow)(nil)
	_ tui.AppBinder       = (*Workflow)(nil)
	_ tui.WatcherProvider = (*Workflow)(nil)
)
