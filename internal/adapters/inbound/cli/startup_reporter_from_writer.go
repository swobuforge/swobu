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

func startupReporterFromWriter(out io.Writer) daemonlifecycle.StartupReporter {
	return writerStartupReporter{out: out}
}

func (r writerStartupReporter) Report(event daemonlifecycle.StartupEvent) {
	if r.out == nil {
		return
	}

	switch event.Kind {
	case daemonlifecycle.StartupEventSplash:
		writeRawLines(r.out, splashLines())
	case daemonlifecycle.StartupEventDaemonNotReachable:
		_, _ = fmt.Fprintf(r.out, "checking: daemon not reachable at %s\n", event.DaemonURL)
	case daemonlifecycle.StartupEventStartingDaemon:
		_, _ = io.WriteString(r.out, "starting: starting daemon\n")
	case daemonlifecycle.StartupEventWaitingReadiness:
		_, _ = io.WriteString(r.out, "waiting: waiting for daemon readiness\n")
	case daemonlifecycle.StartupEventDaemonReady:
		_, _ = fmt.Fprintf(r.out, "ready: daemon ready (%s)\n", event.State)
	case daemonlifecycle.StartupEventStartupFailed:
		writeNoticeBlock(r.out, "startup failed", noticeRows(event.Text, event.NextAction))
	case daemonlifecycle.StartupEventStartupTimedOut:
		writeNoticeBlock(r.out, "startup timed out", noticeRows(event.Text, event.NextAction))
	}
}

func writeNoticeBlock(out io.Writer, title string, rows []string) {
	if out == nil {
		return
	}
	_, _ = fmt.Fprintf(out, "╭─ %s \n", title)
	for _, row := range rows {
		trimmed := strings.TrimSpace(row) // swobu:io-string source=boundary
		if trimmed == "" {
			continue
		}
		_, _ = fmt.Fprintln(out, trimmed)
	}
}

func writePlainLines(out io.Writer, rows []string) {
	if out == nil {
		return
	}
	for _, row := range rows {
		trimmed := strings.TrimSpace(row) // swobu:io-string source=boundary
		if trimmed == "" {
			continue
		}
		_, _ = fmt.Fprintln(out, trimmed)
	}
}

func writeRawLines(out io.Writer, rows []string) {
	if out == nil {
		return
	}
	for _, row := range rows {
		_, _ = fmt.Fprintln(out, row)
	}
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
