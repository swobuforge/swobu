package cli

import (
	"io"

	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
	appstate "github.com/swobuforge/swobu/internal/terminalui/apps/cli/app/state"
	appviews "github.com/swobuforge/swobu/internal/terminalui/apps/cli/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/output"
)

type StartupEventKind = appstate.EventKind

const (
	StartupEventSplash               StartupEventKind = appstate.EventSplash
	StartupEventDisclosure           StartupEventKind = appstate.EventDisclosure
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

func (t *StartupConsolePresenter) Emit(event StartupEvent) {
	t.state = appstate.Apply(t.state, event)
	t.renderer.SetMode(t.state.Mode)
	t.renderer.Render(appviews.Build(t.state))
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
	transcript *StartupTranscript
}

func (r daemonLifecycleStartupReporter) Report(event daemonlifecycle.StartupEvent) {
	if r.transcript == nil {
		return
	}
	r.transcript.EmitDaemonLifecycle(event)
}

// DaemonLifecycleReporter returns a daemon lifecycle reporter that writes
// directly into this transcript.
func (t *StartupConsolePresenter) DaemonLifecycleReporter() daemonlifecycle.StartupReporter {
	return daemonLifecycleStartupReporter{transcript: t}
}

// Backward compatibility shims.
type StartupTranscript = StartupConsolePresenter

func NewStartupTranscript(out io.Writer) *StartupTranscript {
	return NewStartupConsolePresenter(out)
}
