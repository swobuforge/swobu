package route_edit

import (
	"context"
	"errors"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// SaveFunc is the narrow command boundary for route edit/default actions.
type SaveFunc func(context.Context, ports.SaveRouteRequest) (readmodel.RouteReadModel, error)

// DeleteFunc is the narrow command boundary for route delete.
type DeleteFunc func(context.Context, ports.DeleteRouteRequest) error

type Phase int

const (
	PhaseViewing Phase = iota
	PhaseEditing
	PhaseConfirmingDelete
	PhaseSubmitting
	PhaseFailed
)

// Workflow owns expanded route detail edit/default/delete state.
type Workflow struct {
	WorkspaceID readmodel.WorkspaceID
	Route       readmodel.RouteReadModel
	Phase       *tui.State[Phase]
	ModelName   *tui.State[string]
	Error       *tui.State[string]
	Save        SaveFunc
	Delete      DeleteFunc
	OnSaved     func(readmodel.RouteReadModel)
	OnDeleted   func(readmodel.RouteID)
	OnClose     func()
}

func NewWorkflow(workspaceID readmodel.WorkspaceID, route readmodel.RouteReadModel, save SaveFunc, delete DeleteFunc, onSaved func(readmodel.RouteReadModel), onDeleted func(readmodel.RouteID), onClose func()) *Workflow {
	return &Workflow{
		WorkspaceID: workspaceID,
		Route:       route,
		Phase:       tui.NewState(PhaseViewing),
		ModelName:   tui.NewState(route.ModelName),
		Error:       tui.NewState(""),
		Save:        save,
		Delete:      delete,
		OnSaved:     onSaved,
		OnDeleted:   onDeleted,
		OnClose:     onClose,
	}
}

func (w *Workflow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*Workflow)
	if !ok {
		return
	}
	w.WorkspaceID = f.WorkspaceID
	w.Route = f.Route
	w.Save = f.Save
	w.Delete = f.Delete
	w.OnSaved = f.OnSaved
	w.OnDeleted = f.OnDeleted
	w.OnClose = f.OnClose
	if !w.IsEditing() {
		w.ModelName.Set(f.Route.ModelName)
	}
}

func (w *Workflow) KeyMap() tui.KeyMap {
	if !w.IsOpen() {
		return nil
	}
	return tui.KeyMap{
		tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) { w.Back() }),
	}
}

func (w *Workflow) Back() bool {
	if !w.IsOpen() {
		return false
	}
	w.ModelName.Set(w.Route.ModelName)
	w.Error.Set("")
	w.Phase.Set(PhaseViewing)
	if w.OnClose != nil {
		w.OnClose()
	}
	return true
}

func (w *Workflow) IsOpen() bool {
	return w.Phase.Get() == PhaseEditing || w.Phase.Get() == PhaseConfirmingDelete || w.Phase.Get() == PhaseSubmitting || w.Phase.Get() == PhaseFailed
}

func (w *Workflow) IsEditing() bool {
	return w.Phase.Get() == PhaseEditing || w.Phase.Get() == PhaseSubmitting || w.Phase.Get() == PhaseFailed
}

func (w *Workflow) ActivateName() {
	if !w.IsEditing() {
		w.Error.Set("")
		w.Phase.Set(PhaseEditing)
		return
	}
	if strings.TrimSpace(w.ModelName.Get()) == strings.TrimSpace(w.Route.ModelName) { // swobu:io-string source=boundary
		w.Phase.Set(PhaseViewing)
		w.Error.Set("")
		return
	}
	w.Submit(context.Background())
}

func (w *Workflow) Submit(ctx context.Context) {
	modelName, err := w.normalizedModelName()
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
		return
	}
	if w.Save == nil {
		w.Error.Set("route save is not wired yet")
		w.Phase.Set(PhaseFailed)
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseSubmitting)
	route, err := w.Save(ctx, ports.SaveRouteRequest{
		WorkspaceID: w.WorkspaceID,
		RouteID:     w.Route.ID,
		ModelName:   modelName,
		Enabled:     w.Route.Enabled,
	})
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
		return
	}
	w.acceptSavedRoute(route)
	w.Phase.Set(PhaseViewing)
}

func (w *Workflow) ActivateDefault() {
	w.SetDefault(context.Background())
}

func (w *Workflow) SetDefault(ctx context.Context) {
	if w.Route.Default {
		return
	}
	modelName, err := w.normalizedModelName()
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
		return
	}
	if w.Save == nil {
		w.Error.Set("route save is not wired yet")
		w.Phase.Set(PhaseFailed)
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseSubmitting)
	route, err := w.Save(ctx, ports.SaveRouteRequest{
		WorkspaceID: w.WorkspaceID,
		RouteID:     w.Route.ID,
		ModelName:   modelName,
		Enabled:     w.Route.Enabled,
		Default:     true,
	})
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
		return
	}
	route.Default = true
	w.acceptSavedRoute(route)
	w.Phase.Set(PhaseViewing)
}

func (w *Workflow) ActivateDelete() {
	if w.Phase.Get() == PhaseConfirmingDelete {
		w.ConfirmDelete(context.Background())
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseConfirmingDelete)
}

func (w *Workflow) ConfirmDelete(ctx context.Context) {
	if w.Delete == nil {
		w.Error.Set("route delete is not wired yet")
		w.Phase.Set(PhaseFailed)
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseSubmitting)
	if err := w.Delete(ctx, ports.DeleteRouteRequest{
		WorkspaceID: w.WorkspaceID,
		RouteID:     w.Route.ID,
	}); err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseFailed)
		return
	}
	if w.OnDeleted != nil {
		w.OnDeleted(w.Route.ID)
	}
	w.Phase.Set(PhaseViewing)
}

func (w *Workflow) ActionLabel() string {
	if w.IsEditing() {
		return "save ↵"
	}
	return "edit ↵"
}

func (w *Workflow) DefaultValueLabel() string {
	if w.Route.Default {
		return "yes"
	}
	return "no"
}

func (w *Workflow) DefaultActionLabel() string {
	if w.Route.Default {
		return "current"
	}
	return "make default ↵"
}

func (w *Workflow) DeleteValueLabel() string {
	if w.Phase.Get() == PhaseConfirmingDelete {
		return "delete " + w.Route.ModelName + "?"
	}
	return w.Route.ModelName
}

func (w *Workflow) DeleteActionLabel() string {
	if w.Phase.Get() == PhaseConfirmingDelete {
		return "confirm ↵"
	}
	return "delete ↵"
}

func (w *Workflow) normalizedModelName() (string, error) {
	modelName := strings.TrimSpace(w.ModelName.Get()) // swobu:io-string source=boundary
	if modelName == "" {
		return "", errors.New("enter a route model")
	}
	return modelName, nil
}

func (w *Workflow) acceptSavedRoute(route readmodel.RouteReadModel) {
	w.Route = route
	w.ModelName.Set(route.ModelName)
	if w.OnSaved != nil {
		w.OnSaved(route)
	}
}
