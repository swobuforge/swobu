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
	Workspace   readmodel.WorkspaceReadModel
	Phase       *tui.State[Phase]
	Mode        *tui.State[Mode]
	WorkspaceID *tui.State[readmodel.WorkspaceID]
	Slug        *tui.State[string]
	Error       *tui.State[string]
	Save        SaveFunc
	OnSaved     func(readmodel.WorkspaceReadModel)
}

func NewWorkflow(workspace readmodel.WorkspaceReadModel, save SaveFunc, onSaved func(readmodel.WorkspaceReadModel)) *Workflow {
	workflow := &Workflow{
		Workspace:   workspace,
		Phase:       tui.NewState(PhaseViewing),
		Mode:        tui.NewState(ModeEdit),
		WorkspaceID: tui.NewState(workspace.ID),
		Slug:        tui.NewState(workspace.Slug),
		Error:       tui.NewState(""),
		Save:        save,
		OnSaved:     onSaved,
	}
	if workspace.IsDraft() {
		workflow.Mode.Set(ModeCreate)
		workflow.Phase.Set(PhaseEditing)
	}
	return workflow
}

func (w *Workflow) OpenEditor(workspace readmodel.WorkspaceReadModel) {
	w.Workspace = workspace
	w.Mode.Set(ModeEdit)
	w.WorkspaceID.Set(workspace.ID)
	w.Slug.Set(workspace.Slug)
	w.Error.Set("")
	w.Phase.Set(PhaseEditing)
}

func (w *Workflow) OpenCreate() {
	w.Workspace = readmodel.WorkspaceReadModel{State: readmodel.WorkspaceDraft}
	w.Mode.Set(ModeCreate)
	w.WorkspaceID.Set(readmodel.WorkspaceID(""))
	w.Slug.Set("")
	w.Error.Set("")
	w.Phase.Set(PhaseEditing)
}

func (w *Workflow) Back() bool {
	if !w.IsEditing() {
		return false
	}
	w.cancel()
	return true
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

func (w *Workflow) IsEditing() bool {
	return w.Phase.Get() == PhaseEditing || w.Phase.Get() == PhaseFailed || w.Phase.Get() == PhaseSubmitting
}

func (w *Workflow) Activate() {
	if w.Mode.Get() == ModeEdit && !w.IsEditing() {
		w.Phase.Set(PhaseEditing)
		return
	}
	if w.invalidMessage() != "" {
		return
	}
	if w.Mode.Get() == ModeEdit && strings.TrimSpace(w.Slug.Get()) == w.Workspace.Slug {
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

func (w *Workflow) ActionLabel() string {
	if w.invalidMessage() != "" {
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
	return w.invalidMessage()
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

func (w *Workflow) invalidMessage() string {
	slug := strings.TrimSpace(w.Slug.Get())
	if slug == "" {
		return ""
	}
	if _, err := NormalizeSlug(slug); err != nil {
		return err.Error()
	}
	return ""
}

// NormalizeSlug validates the product-level workspace slug shape used by the
// Cockpit workflow before command assembly.
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
