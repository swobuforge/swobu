package run_once

import (
	"context"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type ExecuteFunc func(context.Context, ports.ExecuteRunCommandRequest) (ports.RunExecutionResult, error)

type Phase int

const (
	PhaseReady Phase = iota
	PhaseRunning
	PhaseSucceeded
	PhaseFailed
)

type Workflow struct {
	WorkspaceID readmodel.WorkspaceID
	Command     readmodel.RunCommandReadModel
	Routes      []readmodel.RouteReadModel
	Selected    *tui.State[readmodel.RouteID]
	Picker      *tui.State[bool]
	Phase       *tui.State[Phase]
	Message     *tui.State[string]
	Execute     ExecuteFunc
	OnClose     func()
}

func NewWorkflow(workspace readmodel.WorkspaceReadModel, command readmodel.RunCommandReadModel, execute ExecuteFunc, onClose func()) *Workflow {
	return &Workflow{
		WorkspaceID: workspace.ID,
		Command:     command,
		Routes:      workspace.Routes,
		Selected:    tui.NewState(initialRoute(command, workspace.Routes)),
		Picker:      tui.NewState(false),
		Phase:       tui.NewState(PhaseReady),
		Message:     tui.NewState(""),
		Execute:     execute,
		OnClose:     onClose,
	}
}

func (w *Workflow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*Workflow)
	if !ok {
		return
	}
	w.WorkspaceID = f.WorkspaceID
	w.Command = f.Command
	w.Routes = f.Routes
	w.Execute = f.Execute
	w.OnClose = f.OnClose
}

func (w *Workflow) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) { w.Back() }),
	}
}

func (w *Workflow) Back() bool {
	if w.Picker.Get() {
		w.Picker.Set(false)
		return true
	}
	if w.OnClose != nil {
		w.OnClose()
	}
	return true
}

func (w *Workflow) IsPickerOpen() bool {
	return w.Picker.Get()
}

func (w *Workflow) ToggleModelPicker() {
	if len(w.Routes) == 0 {
		return
	}
	w.Picker.Set(!w.Picker.Get())
}

func (w *Workflow) SelectModel(routeID readmodel.RouteID) {
	if routeID == "" {
		return
	}
	for _, route := range w.Routes {
		if route.ID == routeID {
			w.Selected.Set(routeID)
			w.Picker.Set(false)
			return
		}
	}
}

func (w *Workflow) Run(ctx context.Context) {
	if w.Execute == nil {
		w.Phase.Set(PhaseFailed)
		w.Message.Set("run executor is not wired yet")
		return
	}
	w.Phase.Set(PhaseRunning)
	w.Message.Set("")
	result, err := w.Execute(ctx, ports.ExecuteRunCommandRequest{
		WorkspaceID:  w.WorkspaceID,
		RunCommandID: w.Command.ID,
		RouteID:      w.Selected.Get(),
	})
	if err != nil {
		w.Phase.Set(PhaseFailed)
		w.Message.Set(err.Error())
		return
	}
	w.Phase.Set(PhaseSucceeded)
	if result.ActivityID != "" {
		w.Message.Set("started · " + string(result.ActivityID))
		return
	}
	w.Message.Set("started")
}

func (w *Workflow) ActivateRun() {
	w.Run(context.Background())
}

func (w *Workflow) Title() string {
	if strings.TrimSpace(w.Command.Label) != "" { // swobu:io-string source=boundary
		return w.Command.Label + " run"
	}
	return "run once"
}

func (w *Workflow) ModelValue() string {
	route := w.selectedRoute()
	if route.ID == "" {
		return "_"
	}
	value := route.ModelName
	if value == "" {
		value = string(route.ID)
	}
	if route.Default {
		return value + " default"
	}
	return value
}

func (w *Workflow) CommandValue() string {
	if strings.TrimSpace(w.Command.CommandPreview) != "" { // swobu:io-string source=boundary
		return w.Command.CommandPreview
	}
	if strings.TrimSpace(w.Command.CommandName) != "" { // swobu:io-string source=boundary
		return w.Command.CommandName
	}
	return string(w.Command.ID)
}

func (w *Workflow) RunActionLabel() string {
	if w.Phase.Get() == PhaseRunning {
		return "running"
	}
	return "run ↵"
}

func (w *Workflow) ModelActionLabel() string {
	if w.IsPickerOpen() {
		return "close ↵"
	}
	return "choose ↵"
}

func (w *Workflow) ModelOptionActionLabel() string {
	return "select ↵"
}

func initialRoute(command readmodel.RunCommandReadModel, routes []readmodel.RouteReadModel) readmodel.RouteID {
	if command.TargetRouteID != "" {
		for _, route := range routes {
			if route.ID == command.TargetRouteID {
				return route.ID
			}
		}
	}
	for _, route := range routes {
		if route.Default {
			return route.ID
		}
	}
	if len(routes) > 0 {
		return routes[0].ID
	}
	return command.TargetRouteID
}

func (w *Workflow) selectedRoute() readmodel.RouteReadModel {
	for _, route := range w.Routes {
		if route.ID == w.Selected.Get() {
			return route
		}
	}
	return readmodel.RouteReadModel{ID: w.Selected.Get(), ModelName: w.Command.TargetLabel}
}

func ModelRowComponent(w *Workflow) *ui.SelectableRow {
	row := ui.NewSelectableRow(
		"run-once:"+string(w.Command.ID)+":model",
		"model",
		w.ModelValue(),
		w.ModelActionLabel(),
		func() { w.ToggleModelPicker() },
	)
	return row
}

func ModelOptionRowComponent(w *Workflow, idx int, route readmodel.RouteReadModel) *ui.SelectableRow {
	row := ui.NewSelectableRow(
		"run-once:"+string(w.Command.ID)+":model-option:"+string(route.ID),
		route.ModelName,
		route.RowValue(),
		w.ModelOptionActionLabel(),
		func() { w.SelectModel(route.ID) },
	)
	row.AutoFocus = idx == 0 && w.IsPickerOpen()
	return row
}
