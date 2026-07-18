package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
)

type writerStartupReporter struct {
	out io.Writer
}

type startupReporterWithoutSplash struct {
	next daemonlifecycle.StartupReporter
}

func startupReporterFromWriter(out io.Writer) daemonlifecycle.StartupReporter {
	return writerStartupReporter{out: out}
}

func withoutStartupSplash(next daemonlifecycle.StartupReporter) daemonlifecycle.StartupReporter {
	return startupReporterWithoutSplash{next: next}
}

func (r startupReporterWithoutSplash) Report(event daemonlifecycle.StartupEvent) {
	if event.Kind == daemonlifecycle.StartupEventSplash || r.next == nil {
		return
	}
	r.next.Report(event)
}

func (r writerStartupReporter) Report(event daemonlifecycle.StartupEvent) {
	if r.out == nil {
		return
	}

	switch event.Kind {
	case daemonlifecycle.StartupEventSplash:
		writeRawLines(r.out, splashLines())
	case daemonlifecycle.StartupEventDaemonNotReachable:
		writeStartupLine(r.out, "checking: daemon not reachable at "+event.Addr)
	case daemonlifecycle.StartupEventStartingDaemon:
		writeStartupLine(r.out, "starting: starting daemon")
	case daemonlifecycle.StartupEventWaitingReadiness:
		writeStartupLine(r.out, "waiting: waiting for daemon readiness")
	case daemonlifecycle.StartupEventDaemonReady:
		writeStartupLine(r.out, fmt.Sprintf("ready: daemon ready (%s)", event.State))
	case daemonlifecycle.StartupEventStartupFailed:
		writeNoticeBlock(r.out, "Startup Failed", noticeRows(event.Text, event.NextAction))
	case daemonlifecycle.StartupEventStartupTimedOut:
		writeNoticeBlock(r.out, "Startup Timed Out", noticeRows(event.Text, event.NextAction))
	}
}

func writeRawLines(out io.Writer, rows []string) {
	if out == nil {
		return
	}
	for _, row := range rows {
		writeStartupLine(out, row)
	}
}

func writeStartupLine(out io.Writer, line string) {
	if out == nil {
		return
	}
	_, _ = io.WriteString(out, line+"\r\n")
}

func noticeRows(text string, nextActions []string) []string {
	rows := make([]string, 0, 1+len(nextActions))
	if trimmed := strings.TrimSpace(text); trimmed != "" { // swobu:io-string source=boundary
		rows = append(rows, trimmed)
	}
	for _, next := range nextActions {
		trimmed := strings.TrimSpace(next) // swobu:io-string source=boundary
		if trimmed == "" {
			continue
		}
		rows = append(rows, "next: "+trimmed)
	}
	return rows
}

func splitNoticeRows(text string) []string {
	rows := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") { // swobu:io-string source=boundary
		trimmed := strings.TrimSpace(line) // swobu:io-string source=boundary
		if trimmed == "" {
			continue
		}
		rows = append(rows, trimmed)
	}
	return rows
}

func splashLines() []string {
	return []string{
		" ",
		"                     ___.          ",
		"  ________  _  ______\\_ |__  __ __ ",
		" /  ___/\\ \\/ \\/ /  _ \\| __ \\|  |  \\",
		" \\___  \\ \\     (  <_> ) \\_\\ \\  |  /",
		"/____  /  \\/\\_/ \\____/|___  /____/ ",
		"     \\/                   \\/",
		" ",
	}
}
