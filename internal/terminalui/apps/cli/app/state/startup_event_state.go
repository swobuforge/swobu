package state

import (
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
	"github.com/swobuforge/swobu/internal/terminalui/engine/model"
)

type EventKind = daemonlifecycle.StartupEventKind

const (
	EventSplash               EventKind = daemonlifecycle.StartupEventSplash
	EventDisclosure           EventKind = daemonlifecycle.StartupEventDisclosure
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
	Phase string
}

type StartupState struct {
	SplashPrinted     bool
	DisclosurePrinted bool
	Sections          []SectionRow
	Status            string
	Mode              model.Mode
}

func Initial() StartupState {
	return StartupState{Mode: model.ModeAppend}
}

func Apply(current StartupState, event Event) StartupState {
	next := current
	switch event.Kind {
	case EventSplash:
		if next.SplashPrinted {
			return next
		}
		next.SplashPrinted = true
		next.Sections = append(next.Sections, SectionRow{Kind: "splash", Title: "SWOBU", Rows: []string{"unbundle clients from model backends"}})
	case EventDisclosure:
		if next.DisclosurePrinted {
			return next
		}
		next.DisclosurePrinted = true
		next.Sections = append(next.Sections, SectionRow{Kind: "message", Title: "startup disclosure", Rows: []string{
			"operator startup output is append-only",
			"machine status remains `swobu status` JSON",
			"daemon logs remain in the daemon log sink",
		}})
	case EventTelemetryDisclosure:
		rows := make([]string, 0)
		for _, line := range strings.Split(strings.TrimSpace(event.Text), "\n") {
			if strings.TrimSpace(line) != "" {
				rows = append(rows, line)
			}
		}
		next.Sections = append(next.Sections, SectionRow{Kind: "message", Title: "telemetry disclosure", Rows: rows})
	case EventVersionNotice:
		rows := make([]string, 0)
		for _, line := range strings.Split(strings.TrimSpace(event.Text), "\n") {
			if strings.TrimSpace(line) != "" {
				rows = append(rows, line)
			}
		}
		next.Sections = append(next.Sections, SectionRow{Kind: "message", Title: "version update notice", Rows: rows})
	case EventDaemonNotReachable:
		next.Mode = model.ModeAppend
		next.Status = ""
		next.Sections = append(next.Sections, SectionRow{Kind: "status", Phase: "checking", Rows: []string{"daemon not reachable at " + strings.TrimSpace(event.DaemonURL)}})
	case EventStartingDaemon:
		next.Mode = model.ModeAppend
		next.Status = ""
		next.Sections = append(next.Sections, SectionRow{Kind: "status", Phase: "starting", Rows: []string{"starting daemon"}})
	case EventWaitingReadiness:
		next.Mode = model.ModeLive
		next.Status = "waiting for daemon readiness"
		next.Sections = append(next.Sections, SectionRow{Kind: "status", Phase: "waiting", Rows: []string{"waiting for daemon readiness"}})
	case EventDaemonReady:
		next.Mode = model.ModeAppend
		next.Status = ""
		next.Sections = append(next.Sections, SectionRow{Kind: "status", Phase: "ready", Rows: []string{"daemon ready (" + strings.TrimSpace(event.State) + ")"}})
	case EventDaemonRuntimeStart:
		next.Mode = model.ModeAppend
		next.Status = ""
		rows := []string{"starting daemon runtime"}
		if path := strings.TrimSpace(event.ConfigPath); path != "" {
			rows = append(rows, "config path: "+path)
		}
		next.Sections = append(next.Sections, SectionRow{Kind: "message", Title: "daemon runtime", Rows: rows})
	case EventDaemonRuntimeStop:
		next.Mode = model.ModeAppend
		next.Status = ""
		next.Sections = append(next.Sections, SectionRow{Kind: "status", Phase: "stopped", Rows: []string{"daemon runtime stopped"}})
	case EventStartupFailed:
		next.Mode = model.ModeAppend
		next.Status = ""
		rows := []string{strings.TrimSpace(event.Text)}
		for _, n := range event.NextAction {
			if strings.TrimSpace(n) != "" {
				rows = append(rows, "next: "+strings.TrimSpace(n))
			}
		}
		next.Sections = append(next.Sections, SectionRow{Kind: "message", Title: "startup failed", Rows: rows})
	case EventStartupTimedOut:
		next.Mode = model.ModeAppend
		next.Status = ""
		rows := []string{strings.TrimSpace(event.Text)}
		for _, n := range event.NextAction {
			if strings.TrimSpace(n) != "" {
				rows = append(rows, "next: "+strings.TrimSpace(n))
			}
		}
		next.Sections = append(next.Sections, SectionRow{Kind: "message", Title: "startup timed out", Rows: rows})
	case EventHandoffToInteractive:
		next.Mode = model.ModeAppend
		next.Status = ""
		next.Sections = append(next.Sections, SectionRow{Kind: "status", Phase: "handoff", Rows: []string{"entering interactive cockpit"}})
	}
	return next
}
