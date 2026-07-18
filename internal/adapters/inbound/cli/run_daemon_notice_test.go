package cli

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
	"github.com/swobuforge/swobu/internal/bootstrap"
	"github.com/swobuforge/swobu/internal/telemetry"
)

func TestRunner_DaemonShowsNoticeBeforeStart(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	startCalled := false
	runner := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Start: func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error) {
			startCalled = true
			state, err := telemetry.NewStore().LoadOrCreate()
			if err != nil {
				t.Fatalf("LoadOrCreate returned error: %v", err)
			}
			if !state.NoticeShown {
				t.Fatal("notice_shown = false before daemon start")
			}
			return nil, fmt.Errorf("stop after notice check")
		},
	}

	exitCode := runner.Run(context.Background(), []string{"daemon", "--config", "/tmp/swobu-config.yaml"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if !startCalled {
		t.Fatal("daemon start was not called")
	}
	if stdout.String() == "" {
		t.Fatal("stdout is empty, want first-run notice")
	}
	out := stdout.String()
	splashIdx := strings.Index(out, "___.          ")
	if splashIdx < 0 {
		t.Fatalf("stdout missing splash; stdout=%q", out)
	}
	requireClosedNotice(t, out, "Telemetry Disclosure", nil)
	if telemetryIdx := strings.Index(out, "╭─ Telemetry Disclosure "); telemetryIdx >= 0 && splashIdx > telemetryIdx {
		t.Fatalf("splash must render before telemetry disclosure; stdout=%q", out)
	}
	if !strings.Contains(out, "starting daemon runtime") {
		t.Fatalf("stdout missing daemon runtime narration; stdout=%q", out)
	}
	requireClosedNotice(t, out, "Daemon Runtime", []string{"config path: /tmp/swobu-config.yaml"})
}

func TestStartupReporter_UsesCarriageReturnsForSplashAndReady(t *testing.T) {
	var out bytes.Buffer
	reporter := startupReporterFromWriter(&out)

	reporter.Report(daemonlifecycle.StartupEvent{Kind: daemonlifecycle.StartupEventSplash})
	reporter.Report(daemonlifecycle.StartupEvent{Kind: daemonlifecycle.StartupEventDaemonReady, State: "healthy"})

	text := out.String()
	if !strings.Contains(text, "                     ___.          \r\n") {
		t.Fatalf("splash line missing CRLF reset; stdout=%q", text)
	}
	if !strings.Contains(text, "ready: daemon ready (healthy)\r\n") {
		t.Fatalf("ready line missing CRLF reset; stdout=%q", text)
	}
	withoutCRLF := strings.ReplaceAll(text, "\r\n", "")
	if strings.Contains(withoutCRLF, "\n") {
		t.Fatalf("startup output contains bare LF; stdout=%q", text)
	}
}

func TestRunner_DaemonSkipsTelemetryNoticeWhenEnvSet(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))
	t.Setenv("SWOBU_SKIP_TELEMETRY_NOTICE", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Start: func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error) {
			return nil, fmt.Errorf("stop after notice check")
		},
	}

	exitCode := runner.Run(context.Background(), []string{"daemon", "--config", "/tmp/swobu-config.yaml"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if strings.Contains(stdout.String(), "Telemetry Disclosure") {
		t.Fatalf("stdout should not contain telemetry disclosure when skip env is set; stdout=%q", stdout.String())
	}
}
