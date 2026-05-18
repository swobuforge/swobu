package state

import (
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
	"github.com/swobuforge/swobu/internal/terminalui/view"
)

type EventKind = daemonlifecycle.StartupEventKind

const (
	EventSplash               EventKind = daemonlifecycle.StartupEventSplash
	EventTelemetryDisclosure  EventKind = "telemetry_disclosure"
	EventVersionNotice        EventKind = "version_notice"
	EventDaemonNotReachable   EventKind = daemonlifecycle.StartupEventDaemonNotReachable
	EventStartingDaemon       EventKind = daemonlifecycle.StartupEventStartingDaemon
	EventWaitingReadiness     EventKind = daemonlifecycle.StartupEventWaitingReadiness
	EventDaemonReady          EventKind = daemonlifecycle.StartupEventDaemonReady
	EventDaemonRuntimeStart   EventKind = "daemon_runtime_start"
	EventDaemonRuntimeStop    EventKind = "daemon_runtime_stop"
	EventStartupFailed        EventKind = daemonlifecycle.StartupEventStartupFailed
	EventStartupTimedOut      EventKind = daemonlifecycle.StartupEventStartupTimedOut
	EventHandoffToInteractive EventKind = "handoff_to_interactive"
)

type Event struct {
	Kind       EventKind
	Text       string
	DaemonURL  string
	State      string
	ConfigPath string
	NextAction []string
}

type SectionRow struct {
	Kind  string
	Title string
	Rows  []string
}

type StartupState struct {
	SplashPrinted bool
	Sections      []SectionRow
	Mode          view.RenderMode
}

func Initial() StartupState {
	return StartupState{Mode: view.RenderModeAppend}
}

func Apply(current StartupState, event Event) StartupState {
	next := current
	switch event.Kind {
	case EventSplash:
		if next.SplashPrinted {
			return next
		}
		next.SplashPrinted = true
		next.Sections = append(next.Sections, SectionRow{Kind: "splash", Rows: []string{
			" ",
			"                     ___.          ",
			"  ________  _  ______\\_ |__  __ __ ",
			" /  ___/\\ \\/ \\/ /  _ \\| __ \\|  |  \\",
			" \\___  \\ \\     (  <_> ) \\_\\ \\  |  /",
			"/____  /  \\/\\_/ \\____/|___  /____/ ",
			"     \\/                   \\/",
			" ",
		}})
	case EventTelemetryDisclosure:
		rows := make([]string, 0)
		for _, line := range strings.Split(strings.TrimSpace(event.Text), "\n") { // swobu:io-string source=boundary
			if strings.TrimSpace(line) != "" { // swobu:io-string source=boundary
				rows = append(rows, line)
			}
		}
		next.Sections = append(next.Sections, SectionRow{Kind: "message", Title: "telemetry disclosure", Rows: rows})
	case EventVersionNotice:
		rows := make([]string, 0)
		for _, line := range strings.Split(strings.TrimSpace(event.Text), "\n") { // swobu:io-string source=boundary
			if strings.TrimSpace(line) != "" { // swobu:io-string source=boundary
				rows = append(rows, line)
			}
		}
		next.Sections = append(next.Sections, SectionRow{Kind: "message", Title: "Update Available", Rows: rows})
	case EventDaemonNotReachable:
		next.Mode = view.RenderModeAppend
	case EventStartingDaemon:
		next.Mode = view.RenderModeAppend
	case EventWaitingReadiness:
		next.Mode = view.RenderModeAppend
	case EventDaemonReady:
		next.Mode = view.RenderModeAppend
	case EventDaemonRuntimeStart:
		next.Mode = view.RenderModeAppend
		rows := []string{"starting daemon runtime"}
		if path := strings.TrimSpace(event.ConfigPath); path != "" { // swobu:io-string source=boundary
			rows = append(rows, "config path: "+path)
		}
		next.Sections = append(next.Sections, SectionRow{Kind: "message", Title: "Daemon Runtime", Rows: rows})
	case EventDaemonRuntimeStop:
		next.Mode = view.RenderModeAppend
	case EventStartupFailed:
		next.Mode = view.RenderModeAppend
		rows := []string{strings.TrimSpace(event.Text)} // swobu:io-string source=boundary
		for _, n := range event.NextAction {
			if strings.TrimSpace(n) != "" { // swobu:io-string source=boundary
				rows = append(rows, "next: "+strings.TrimSpace(n)) // swobu:io-string source=boundary
			}
		}
		next.Sections = append(next.Sections, SectionRow{Kind: "message", Title: "startup failed", Rows: rows})
	case EventStartupTimedOut:
		next.Mode = view.RenderModeAppend
		rows := []string{strings.TrimSpace(event.Text)} // swobu:io-string source=boundary
		for _, n := range event.NextAction {
			if strings.TrimSpace(n) != "" { // swobu:io-string source=boundary
				rows = append(rows, "next: "+strings.TrimSpace(n)) // swobu:io-string source=boundary
			}
		}
		next.Sections = append(next.Sections, SectionRow{Kind: "message", Title: "startup timed out", Rows: rows})
	case EventHandoffToInteractive:
		next.Mode = view.RenderModeAppend
	}
	return next
}
