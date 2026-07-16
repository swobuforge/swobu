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

// Workflow owns the workspace slug lifecycle row: edit for unchanged existing
// workspaces, save for changed existing workspaces, and create for draft
// workspaces.
type Workflow struct {
	ui.SelectBase
	Workspace   readmodel.WorkspaceReadModel
	Phase       *tui.State[Phase]
	Mode        *tui.State[Mode]
	WorkspaceID *tui.State[readmodel.WorkspaceID]
	Slug        *tui.State[string]
	Error       *tui.State[string]
	Save        SaveFunc
	OnSaved     func(readmodel.WorkspaceReadModel)
}

// workflowKey mirrors the section mount key so focus repair sees one stable
// row identity per workspace slot.
func workflowKey(workspace readmodel.WorkspaceReadModel) string {
	if workspace.ID != "" {
		return "workspace-edit:" + string(workspace.ID)
	}
	if workspace.Slug != "" {
		return "workspace-edit:" + workspace.Slug
	}
	return "workspace-edit:+"
}

func (w *Workflow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*Workflow)
	if !ok {
		return
	}
	// Reset the local row state when the workspace identity or lifecycle flips.
	// That prevents a promoted draft row from leaking its submit values into the
	// fresh draft slot after refresh.
	reseed := w.Workspace.ID != f.Workspace.ID || w.Workspace.State != f.Workspace.State
	w.Workspace = f.Workspace
	w.Save = f.Save
	w.OnSaved = f.OnSaved
	if reseed {
		w.seedFromWorkspace(f.Workspace)
	}
}

func NewWorkflow(workspace readmodel.WorkspaceReadModel, save SaveFunc, onSaved func(readmodel.WorkspaceReadModel)) *Workflow {
	workflow := &Workflow{
		SelectBase:  ui.NewSelectBase(workflowKey(workspace)),
		Workspace:   workspace,
		Phase:       tui.NewState(PhaseViewing),
		Mode:        tui.NewState(ModeEdit),
		WorkspaceID: tui.NewState(workspace.ID),
		Slug:        tui.NewState(workspace.Slug),
		Error:       tui.NewState(""),
		Save:        save,
		OnSaved:     onSaved,
	}
	workflow.seedFromWorkspace(workspace)
	return workflow
}

func (w *Workflow) OpenEditor(workspace readmodel.WorkspaceReadModel) {
	w.seedFromWorkspace(workspace)
	w.Mode.Set(ModeEdit)
	w.Phase.Set(PhaseEditing)
	w.OnFocus(nil)
}

func (w *Workflow) OpenCreate() {
	w.seedFromWorkspace(readmodel.WorkspaceReadModel{State: readmodel.WorkspaceDraft})
	w.OnFocus(nil)
}

// BindApp wires the workflow's focus state into the app so the selected row can
// redraw its marker when focus changes.
func (w *Workflow) BindApp(app *tui.App) {
	w.SelectBase.BindApp(app)
}

// UnbindApp releases the cached app handle when the workflow leaves the tree.
func (w *Workflow) UnbindApp() {
	w.SelectBase.UnbindApp()
}

func (w *Workflow) Back() bool {
	if !w.IsEditing() {
		return false
	}
	w.cancel()
	return true
}

func (w *Workflow) KeyMap() tui.KeyMap {
	if !w.IsEditing() {
		return nil
	}
	return tui.KeyMap{
		tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) { w.Back() }),
	}
}

func (w *Workflow) closeEdit() {
	w.Error.Set("")
	if w.Mode.Get() == ModeCreate {
		w.Phase.Set(PhaseEditing)
		return
	}
	w.Phase.Set(PhaseViewing)
}

func (w *Workflow) cancel() {
	w.Error.Set("")
	w.Slug.Set(w.Workspace.Slug)
	w.closeEdit()
}

func (w *Workflow) seedFromWorkspace(workspace readmodel.WorkspaceReadModel) {
	w.Workspace = workspace
	w.SelectBase.ID = workflowKey(workspace)
	w.WorkspaceID.Set(workspace.ID)
	w.Slug.Set(workspace.Slug)
	w.Error.Set("")
	if workspace.IsDraft() {
		w.Mode.Set(ModeCreate)
		w.Phase.Set(PhaseEditing)
		w.OnFocus(nil)
		return
	}
	w.Mode.Set(ModeEdit)
	w.Phase.Set(PhaseViewing)
}

func (w *Workflow) IsEditing() bool {
	return w.Phase.Get() == PhaseEditing || w.Phase.Get() == PhaseFailed || w.Phase.Get() == PhaseSubmitting
}

// Arrow returns the row marker for the workspace slug interaction scope.
// Editing means the mounted input is the active descendant of this row; the
// row marker remains the same selection grammar used by view-mode rows.
func (w *Workflow) Arrow() string {
	return w.ArrowWithActiveDescendant(w.IsEditing())
}

func (w *Workflow) Activate() {
	if w.Mode.Get() == ModeEdit && !w.IsEditing() {
		w.Phase.Set(PhaseEditing)
		w.OnFocus(nil)
		return
	}
	if w.ErrorMessage() != "" {
		return
	}
	slug := strings.TrimSpace(w.Slug.Get()) // swobu:io-string source=boundary
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
		return
	}
	if w.Mode.Get() == ModeCreate {
		if w.Save == nil {
			w.Error.Set("workspace save is not wired yet")
			w.Phase.Set(PhaseFailed)
			return
		}
		w.Error.Set("")
		w.Phase.Set(PhaseSubmitting)
		workspace, err := w.Save(ctx, ports.SaveWorkspaceRequest{
			ID:   w.WorkspaceID.Get(),
			Slug: slug,
		})
		if err != nil {
			w.Error.Set(err.Error())
			w.Phase.Set(PhaseFailed)
			return
		}
		w.Workspace = workspace
		w.WorkspaceID.Set(workspace.ID)
		w.Slug.Set(workspace.Slug)
		w.Mode.Set(ModeEdit)
		w.closeEdit()
		if w.OnSaved != nil {
			w.OnSaved(workspace)
		}
		return
	}
	if w.Save == nil {
		w.Error.Set("workspace save is not wired yet")
		w.Phase.Set(PhaseFailed)
		return
	}

	w.Error.Set("")
	w.Phase.Set(PhaseSubmitting)
	workspace, err := w.Save(ctx, ports.SaveWorkspaceRequest{
		ID:   w.WorkspaceID.Get(),
		Slug: slug,
	})
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
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
}

func (w *Workflow) SubmitSlug(value string) {
	w.Slug.Set(value)
	w.Submit(context.Background())
}

func (w *Workflow) ActionLabel() string {
	if w.visibleError() != "" {
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
	if w.Slug.Get() != "" {
		return w.Slug.Get()
	}
	if w.Mode.Get() == ModeCreate {
		return "_"
	}
	return w.Workspace.Slug
}

func (w *Workflow) ErrorMessage() string {
	slug := strings.TrimSpace(w.Slug.Get()) // swobu:io-string source=boundary
	if slug == "" {
		return ""
	}
	if _, err := NormalizeSlug(slug); err != nil {
		return err.Error()
	}
	return ""
}

// visibleError keeps validation and submit failures on the row itself.
// Without this, a rejected save reads like a no-op and the PTY proof lane
// waits forever for a success state that never arrives.
func (w *Workflow) visibleError() string {
	if msg := w.ErrorMessage(); msg != "" {
		return msg
	}
	return strings.TrimSpace(w.Error.Get())
}

func (w *Workflow) ClientBaseURLPreview() string {
	slug, err := NormalizeSlug(w.Slug.Get())
	if err != nil {
		return "(derived from slug)"
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

// NormalizeSlug validates the product-level workspace slug shape used by the
// Cockpit workflow before command assembly.
func NormalizeSlug(raw string) (string, error) {
	slug := strings.TrimSpace(raw) // swobu:io-string source=boundary
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
