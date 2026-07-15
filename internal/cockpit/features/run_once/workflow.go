package run_once

import (
	"context"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
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
	if w.OnClose != nil {
		w.OnClose()
	}
	return true
}

func (w *Workflow) ChangeModel() {
	if len(w.Routes) == 0 {
		return
	}
	current := w.Selected.Get()
	for i, route := range w.Routes {
		if route.ID != current {
			continue
		}
		w.Selected.Set(w.Routes[(i+1)%len(w.Routes)].ID)
		return
	}
	w.Selected.Set(initialRoute(w.Command, w.Routes))
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

func (w *Workflow) StatusMessage() string {
	return w.Message.Get()
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
