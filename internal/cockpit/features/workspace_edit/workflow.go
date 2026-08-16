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

// RenameFunc is the narrow command boundary for persisted workspace renames.
type RenameFunc func(context.Context, ports.RenameWorkspaceRequest) (readmodel.WorkspaceReadModel, error)

// Phase is the workspace name row lifecycle.
type Phase int

const (
	PhaseViewing Phase = iota
	PhaseEditing
	PhaseSubmitting
	PhaseFailed
)

// Workflow owns the operator-facing workspace name lifecycle row.
//
// The shared EditableRow component owns the focus shell, cursor, and inline
// text editing. Workflow owns local draft naming, persisted rename submission,
// and validation projection that turns the URL-safe name into RFC grammar.
type Workflow struct {
	Workspace   readmodel.WorkspaceReadModel
	Phase       *tui.State[Phase]
	WorkspaceID *tui.State[readmodel.WorkspaceID]
	Slug        *tui.State[string]
	Error       *tui.State[string]
	Rename      RenameFunc
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

func NewWorkflow(workspace readmodel.WorkspaceReadModel, save RenameFunc, onSaved func(readmodel.WorkspaceReadModel)) *Workflow {
	w := &Workflow{
		Workspace:   workspace,
		Phase:       tui.NewState(PhaseViewing),
		WorkspaceID: tui.NewState(workspace.ID),
		Slug:        tui.NewState(workspace.Slug),
		Error:       tui.NewState(""),
		Rename:      save,
		OnSaved:     onSaved,
	}
	w.row = ui.NewEditableRow(workflowKey(workspace), "name", w.Slug)
	w.row.PublishWhileEditing = true
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
	reseed := w.Workspace.ID != f.Workspace.ID || w.Workspace.State != f.Workspace.State || w.Workspace.Slug != f.Workspace.Slug
	w.Workspace = f.Workspace
	w.Rename = f.Rename
	w.OnSaved = f.OnSaved
	if reseed {
		w.seedFromWorkspace(f.Workspace)
	}
	w.syncRow()
}

func (w *Workflow) OpenEditor(workspace readmodel.WorkspaceReadModel) {
	w.seedFromWorkspace(workspace)
	w.Phase.Set(PhaseEditing)
	w.row.Open()
	w.syncRow()
}

func (w *Workflow) OpenDraft() {
	w.seedFromWorkspace(readmodel.NewDraftWorkspace(nil))
	w.row.Open()
	w.syncRow()
}

func (w *Workflow) BindApp(app *tui.App) {
	w.Phase.BindApp(app)
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
	if workspace.IsDraft() && strings.TrimSpace(workspace.Slug) == "" {
		w.Phase.Set(PhaseEditing)
		w.row.Open()
		return
	}
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
	if !w.Workspace.IsDraft() && slug == w.Workspace.Slug {
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
	if w.Workspace.IsDraft() {
		workspace := w.Workspace
		workspace.ID = "+"
		workspace.Slug = slug
		workspace.State = readmodel.WorkspaceDraft
		workspace.WorkspaceURL = w.WorkspaceURLPreview()
		w.finishSubmit(workspace)
		return
	}
	if w.Rename == nil {
		w.Error.Set("workspace save is not wired yet")
		w.Phase.Set(PhaseFailed)
		w.syncRow()
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseSubmitting)
	workspace, err := w.Rename(ctx, ports.RenameWorkspaceRequest{ID: w.WorkspaceID.Get(), Slug: slug})
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
		w.syncRow()
		return
	}
	w.finishSubmit(workspace)
}

func (w *Workflow) finishSubmit(workspace readmodel.WorkspaceReadModel) {
	w.Workspace = workspace
	w.WorkspaceID.Set(workspace.ID)
	w.Slug.Set(workspace.Slug)
	w.closeEdit()
	w.syncRow()
	if w.OnSaved != nil {
		w.OnSaved(workspace)
	}
}

func (w *Workflow) SubmitSlug(value string) {
	w.Slug.Set(value)
	w.Submit(context.Background())
}

func (w *Workflow) ActionLabel() string {
	if msg := strings.TrimSpace(w.ErrorMessage()); msg != "" {
		if msg == "enter a workspace name" {
			return "required"
		}
		if isDuplicateError(msg) {
			return "duplicate"
		}
		return "invalid"
	}
	if w.IsEditing() {
		if w.Workspace.IsDraft() {
			return "continue ↵"
		}
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
		if w.Workspace.IsDraft() {
			return "enter a workspace name"
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

func (w *Workflow) WorkspaceURLPreview() string {
	if msg := strings.TrimSpace(w.ErrorMessage()); msg != "" {
		return "after first target"
	}
	slug, err := NormalizeSlug(w.Slug.Get())
	if err != nil {
		return "after first target"
	}
	baseURL := w.Workspace.WorkspaceURL
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
	w.row.Label = "name"
	w.row.Value = w.Slug
	w.row.ValueWidth = 32
	w.row.Validation = w.rowValidation()
	w.row.ValidationText = w.ErrorMessage()
	if w.Workspace.IsDraft() {
		w.row.ViewAction = "edit ↵"
		w.row.EditAction = "continue ↵"
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
	if msg == "enter a workspace name" {
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
		return "", errors.New("enter a workspace name")
	}
	for _, r := range slug {
		if unicode.IsLower(r) || unicode.IsDigit(r) || r == '-' {
			continue
		}
		return "", fmt.Errorf("use lowercase letters, numbers, and hyphens")
	}
	if slug[0] == '-' || slug[len(slug)-1] == '-' {
		return "", errors.New("name cannot start or end with hyphen")
	}
	if strings.Contains(slug, "--") {
		return "", errors.New("name cannot contain consecutive hyphens")
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
