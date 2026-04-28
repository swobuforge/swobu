package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui"
	"github.com/metrofun/swobu/internal/app/operator/daemonlifecycle"
	"github.com/metrofun/swobu/internal/bootstrap"
	platformconfig "github.com/metrofun/swobu/internal/platform/config"
	"github.com/metrofun/swobu/internal/telemetry"
)

// ExitCode is contract-bearing for `swobu status`: healthy=0, reachable but
// uninitialized=1, and daemon unreachable=2.
type ExitCode int

const (
	ExitHealthy       ExitCode = 0
	ExitUninitialized ExitCode = 1
	ExitDegraded      ExitCode = 1
	ExitDown          ExitCode = 2
)

type StatusPayload = daemonlifecycle.StatusPayload

type Runner struct {
	Stdin             io.Reader
	Stdout            io.Writer
	Stderr            io.Writer
	HTTPClient        *http.Client
	Start             func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error)
	IsInteractive     func() bool
	AttachOrStart     func(context.Context, io.Writer, io.Writer, *http.Client) error
	LaunchInteractive func(context.Context, io.Reader, io.Writer, io.Writer) error
}

// daemon control, explicit lifecycle commands, and TUI launch handoff.
func (r Runner) Run(ctx context.Context, args []string) ExitCode {
	stdin := r.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := r.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := r.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	start := r.Start
	if start == nil {
		start = bootstrap.Start
	}
	isInteractive := r.IsInteractive
	if isInteractive == nil {
		isInteractive = defaultIsInteractive
	}
	launchInteractive := r.LaunchInteractive
	if launchInteractive == nil {
		launchInteractive = tui.Run
	}
	attachOrStart := r.AttachOrStart
	if attachOrStart == nil {
		attachOrStart = defaultAttachOrStart
	}

	if len(args) == 0 {
		if isInteractive() {
			if err := attachOrStart(ctx, stdout, stderr, client); err != nil {
				_, _ = fmt.Fprintln(stderr, err.Error())
				return ExitDown
			}
			if err := launchInteractive(ctx, stdin, stdout, stderr); err != nil {
				_, _ = fmt.Fprintln(stderr, err.Error())
				return ExitDown
			}
			return ExitHealthy
		}
		_, _ = fmt.Fprintln(stderr, "interactive cockpit requires a terminal; use `swobu status` or `swobu daemon --config <path>`")
		return ExitDown
	}

	switch args[0] {
	case "daemon":
		return runDaemon(ctx, start, stdout, stderr, args[1:])
	case "status":
		return runStatus(ctx, client, stdout, stderr, args[1:])
	case "down":
		return runDown(ctx, client, stdout, stderr, args[1:])
	case "telemetry":
		return runTelemetry(stdout, stderr, args[1:])
	default:
		_, _ = fmt.Fprintf(stderr, "unknown subcommand %q\n", args[0])
		return ExitDown
	}
}

func defaultIsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func runDaemon(ctx context.Context, start func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error), _ io.Writer, stderr io.Writer, args []string) ExitCode {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "path to the root daemon config file")
	if err := fs.Parse(args); err != nil {
		return ExitDown
	}
	if *configPath == "" {
		_, _ = fmt.Fprintln(stderr, "--config is required")
		return ExitDown
	}

	logger := slog.Default()
	logger.Info("daemon lifecycle", "component", "daemon", "event", "process_start", "config_path", *configPath)
	daemon, err := start(ctx, bootstrap.StartInput{ConfigPath: *configPath, Logger: logger})
	if err != nil {
		logger.Error("daemon lifecycle", "component", "daemon", "event", "initialization_failed", "error", err.Error())
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	defer func() {
		_ = daemon.Close(context.Background())
		logger.Info("daemon lifecycle", "component", "daemon", "event", "process_stop")
	}()

	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-signalCtx.Done():
		logger.Info("daemon lifecycle", "component", "daemon", "event", "signal_received", "signal", "interrupt_or_sigterm")
		_ = daemon.Close(context.Background())
		if errors.Is(signalCtx.Err(), context.Canceled) {
			return ExitHealthy
		}
		if waitErr := daemon.Wait(context.Background()); waitErr != nil && !errors.Is(waitErr, context.Canceled) {
			_, _ = fmt.Fprintln(stderr, waitErr.Error())
			return ExitDown
		}
		return ExitHealthy
	case <-daemonDone(daemon):
		if waitErr := daemon.Wait(context.Background()); waitErr != nil && !errors.Is(waitErr, context.Canceled) {
			_, _ = fmt.Fprintln(stderr, waitErr.Error())
			return ExitDown
		}
		return ExitHealthy
	}
}

func runStatus(ctx context.Context, client *http.Client, stdout io.Writer, _ io.Writer, args []string) ExitCode {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	daemonURL := fs.String("daemon-url", platformconfig.DefaultDaemonURL(), "daemon base URL")
	if err := fs.Parse(args); err != nil {
		return ExitDown
	}

	payload, exitCode := fetchStatus(ctx, client, *daemonURL)
	_ = json.NewEncoder(stdout).Encode(payload)
	return exitCode
}

func runDown(ctx context.Context, client *http.Client, _ io.Writer, stderr io.Writer, args []string) ExitCode {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	fs.SetOutput(stderr)
	daemonURL := fs.String("daemon-url", platformconfig.DefaultDaemonURL(), "daemon base URL")
	timeout := fs.Duration("timeout", 5*time.Second, "time to wait for graceful shutdown")
	if err := fs.Parse(args); err != nil {
		return ExitDown
	}
	if *timeout <= 0 {
		_, _ = fmt.Fprintln(stderr, "--timeout must be > 0")
		return ExitDown
	}
	result, err := daemonlifecycle.Down(ctx, daemonlifecycle.DownInput{
		DaemonURL: *daemonURL,
		Client:    client,
		Timeout:   *timeout,
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	if result == daemonlifecycle.DownResultAlreadyStopped {
		_, _ = fmt.Fprintln(stderr, "daemon already stopped")
	}
	return ExitHealthy
}

func runTelemetry(stdout io.Writer, stderr io.Writer, args []string) ExitCode {
	store := telemetry.NewStore()
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "telemetry subcommand required: status|on|off|inspect|show-payload|reset")
		return ExitDown
	}

	switch args[0] {
	case "status":
		return runTelemetryStatus(stdout, stderr, store, args[1:])
	case "on":
		return runTelemetrySetEnabled(stdout, stderr, store, true, args[1:])
	case "off":
		return runTelemetrySetEnabled(stdout, stderr, store, false, args[1:])
	case "inspect":
		return runTelemetryInspect(stdout, stderr, store, args[1:])
	case "show-payload":
		return runTelemetryInspect(stdout, stderr, store, args[1:])
	case "reset":
		return runTelemetryReset(stdout, stderr, store, args[1:])
	default:
		_, _ = fmt.Fprintf(stderr, "unknown telemetry subcommand %q\n", args[0])
		return ExitDown
	}
}

func runTelemetryStatus(stdout io.Writer, stderr io.Writer, store telemetry.Store, args []string) ExitCode {
	fs := flag.NewFlagSet("telemetry status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return ExitDown
	}
	state, err := store.LoadOrCreate()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	payload := struct {
		Enabled            bool   `json:"enabled"`
		AnonymousInstallID string `json:"anonymous_install_id"`
		FirstSeenAt        string `json:"first_seen_at"`
		NoticeShown        bool   `json:"notice_shown"`
		LastUploadAt       string `json:"last_upload_at,omitempty"`
	}{
		Enabled:            state.Enabled,
		AnonymousInstallID: state.AnonymousInstallID,
		FirstSeenAt:        state.FirstSeenAt,
		NoticeShown:        state.NoticeShown,
		LastUploadAt:       state.LastUploadAt,
	}
	if err := json.NewEncoder(stdout).Encode(payload); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	return ExitHealthy
}

func runTelemetrySetEnabled(stdout io.Writer, stderr io.Writer, store telemetry.Store, enabled bool, args []string) ExitCode {
	fs := flag.NewFlagSet("telemetry toggle", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return ExitDown
	}
	state, err := store.SetEnabled(enabled)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	payload := struct {
		Enabled bool `json:"enabled"`
	}{
		Enabled: state.Enabled,
	}
	if err := json.NewEncoder(stdout).Encode(payload); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	return ExitHealthy
}

func runTelemetryInspect(stdout io.Writer, stderr io.Writer, store telemetry.Store, args []string) ExitCode {
	fs := flag.NewFlagSet("telemetry inspect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return ExitDown
	}
	preview, err := store.InspectPreview()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	if _, err := stdout.Write(append(preview, '\n')); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	return ExitHealthy
}

func runTelemetryReset(stdout io.Writer, stderr io.Writer, store telemetry.Store, args []string) ExitCode {
	fs := flag.NewFlagSet("telemetry reset", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return ExitDown
	}
	state, err := store.Reset()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	payload := struct {
		Enabled            bool   `json:"enabled"`
		AnonymousInstallID string `json:"anonymous_install_id"`
		FirstSeenAt        string `json:"first_seen_at"`
		NoticeShown        bool   `json:"notice_shown"`
	}{
		Enabled:            state.Enabled,
		AnonymousInstallID: state.AnonymousInstallID,
		FirstSeenAt:        state.FirstSeenAt,
		NoticeShown:        state.NoticeShown,
	}
	if err := json.NewEncoder(stdout).Encode(payload); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	return ExitHealthy
}

func fetchStatus(ctx context.Context, client *http.Client, daemonURL string) (StatusPayload, ExitCode) {
	payload, class := daemonlifecycle.FetchStatus(ctx, client, daemonURL)
	switch class {
	case daemonlifecycle.StatusClassHealthy:
		return payload, ExitHealthy
	case daemonlifecycle.StatusClassUninitialized:
		return payload, ExitUninitialized
	case daemonlifecycle.StatusClassDegraded:
		return payload, ExitDegraded
	default:
		return StatusPayload{State: "down"}, ExitDown
	}
}

func daemonDone(d *bootstrap.Daemon) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		_ = d.Wait(context.Background())
		close(done)
	}()
	return done
}

func defaultAttachOrStart(ctx context.Context, stdout io.Writer, _ io.Writer, client *http.Client) error {
	_, err := daemonlifecycle.AttachOrStart(ctx, daemonlifecycle.AttachOrStartInput{
		DaemonURL:        platformconfig.DefaultDaemonURL(),
		Client:           client,
		Stdout:           stdout,
		ReadinessTimeout: 15 * time.Second,
	})
	return err
}
