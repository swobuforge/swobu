package cli

import (
	"io"
	"log/slog"

	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
	appstate "github.com/swobuforge/swobu/internal/terminalui/apps/cli/app/state"
	appviews "github.com/swobuforge/swobu/internal/terminalui/apps/cli/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/output"
)

type StartupEventKind = appstate.EventKind

const (
	StartupEventSplash               StartupEventKind = appstate.EventSplash
	StartupEventTelemetryDisclosure  StartupEventKind = appstate.EventTelemetryDisclosure
	StartupEventVersionNotice        StartupEventKind = appstate.EventVersionNotice
	StartupEventDaemonNotReachable   StartupEventKind = appstate.EventDaemonNotReachable
	StartupEventStartingDaemon       StartupEventKind = appstate.EventStartingDaemon
	StartupEventWaitingReadiness     StartupEventKind = appstate.EventWaitingReadiness
	StartupEventDaemonReady          StartupEventKind = appstate.EventDaemonReady
	StartupEventDaemonRuntimeStart   StartupEventKind = appstate.EventDaemonRuntimeStart
	StartupEventDaemonRuntimeStop    StartupEventKind = appstate.EventDaemonRuntimeStop
	StartupEventStartupFailed        StartupEventKind = appstate.EventStartupFailed
	StartupEventStartupTimedOut      StartupEventKind = appstate.EventStartupTimedOut
	StartupEventHandoffToInteractive StartupEventKind = appstate.EventHandoffToInteractive
)

type StartupEvent = appstate.Event

type StartupState = appstate.StartupState

// TODO(execution-system): StartupTranscript is a legacy startup presenter noun kept as a type alias
// for compatibility with external integration tests and callers.
type StartupTranscript = StartupConsolePresenter

// StartupConsolePresenter is a thin runtime adapter over cli app/state + app/views.
type StartupConsolePresenter struct {
	renderer *output.Renderer
	state    appstate.StartupState
}

func NewStartupConsolePresenter(out io.Writer) *StartupConsolePresenter {
	return &StartupConsolePresenter{
		renderer: output.NewRenderer(out, appstate.Initial().Mode),
		state:    appstate.Initial(),
	}
}

// TODO(execution-system): NewStartupTranscript keeps a legacy constructor surface while delegating to
// the canonical StartupConsolePresenter constructor.
func NewStartupTranscript(out io.Writer) *StartupConsolePresenter {
	return NewStartupConsolePresenter(out)
}

func (t *StartupConsolePresenter) Emit(event StartupEvent) {
	if isStartupPhaseLogEvent(event.Kind) {
		logStartupPhaseEvent(event)
		return
	}
	t.state = appstate.Apply(t.state, event)
	t.renderer.SetMode(t.state.Mode)
	t.renderer.Render(appviews.Build(t.state))
}

func isStartupPhaseLogEvent(kind StartupEventKind) bool {
	switch kind {
	case StartupEventDaemonNotReachable,
		StartupEventStartingDaemon,
		StartupEventWaitingReadiness,
		StartupEventDaemonReady,
		StartupEventDaemonRuntimeStop,
		StartupEventHandoffToInteractive:
		return true
	default:
		return false
	}
}

func logStartupPhaseEvent(event StartupEvent) {
	switch event.Kind {
	case StartupEventDaemonNotReachable:
		slog.Info("startup phase", "phase", "checking", "message", "daemon not reachable", "daemon_url", event.DaemonURL)
	case StartupEventStartingDaemon:
		slog.Info("startup phase", "phase", "starting", "message", "starting daemon")
	case StartupEventWaitingReadiness:
		slog.Info("startup phase", "phase", "waiting", "message", "waiting for daemon readiness")
	case StartupEventDaemonReady:
		slog.Info("startup phase", "phase", "ready", "message", "daemon ready", "state", event.State)
	case StartupEventDaemonRuntimeStop:
		slog.Info("startup phase", "phase", "stopped", "message", "daemon runtime stopped")
	case StartupEventHandoffToInteractive:
		slog.Info("startup phase", "phase", "handoff", "message", "entering interactive cockpit")
	}
}

// EmitDaemonLifecycle renders daemonlifecycle startup events without
// maintaining a parallel event-switch mapping.
func (t *StartupConsolePresenter) EmitDaemonLifecycle(event daemonlifecycle.StartupEvent) {
	if t == nil {
		return
	}
	t.Emit(StartupEvent{
		Kind:       event.Kind,
		State:      event.State,
		DaemonURL:  event.DaemonURL,
		Text:       event.Text,
		NextAction: append([]string(nil), event.NextAction...),
	})
}

type daemonLifecycleStartupReporter struct {
	presenter *StartupConsolePresenter
}

func (r daemonLifecycleStartupReporter) Report(event daemonlifecycle.StartupEvent) {
	if r.presenter == nil {
		return
	}
	r.presenter.EmitDaemonLifecycle(event)
}

// DaemonLifecycleReporter returns a daemon lifecycle reporter that writes
// directly into this transcript.
func (t *StartupConsolePresenter) DaemonLifecycleReporter() daemonlifecycle.StartupReporter {
	return daemonLifecycleStartupReporter{presenter: t}
}
