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
	"strings"
	"syscall"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
	"github.com/swobuforge/swobu/internal/bootstrap"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	platformlogging "github.com/swobuforge/swobu/internal/platform/logging"
	"github.com/swobuforge/swobu/internal/telemetry"
	uicli "github.com/swobuforge/swobu/internal/terminalui/apps/cli"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit"
	uimode "github.com/swobuforge/swobu/internal/terminalui/session"
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
	Stdin               io.Reader
	Stdout              io.Writer
	Stderr              io.Writer
	HTTPClient          *http.Client
	Start               func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error)
	IsInteractive       func() bool
	AttachOrStart       func(context.Context, io.Writer, io.Writer, *http.Client) error
	LaunchInteractive   func(context.Context, io.Reader, io.Writer, io.Writer) error
	StartupHandoffFloor time.Duration
	Sleep               func(time.Duration)
}

type uiModeBridge interface {
	Mode() uimode.Mode
	OnModeChange(func(prev, next uimode.Mode))
	BindWriter(mode uimode.Mode, writer io.Writer) io.Writer
	SetMode(mode uimode.Mode) error
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
		launchInteractive = cockpit.Run
	}
	attachOrStart := r.AttachOrStart
	if attachOrStart == nil {
		attachOrStart = defaultAttachOrStart
	}
	startupHandoffFloor := r.StartupHandoffFloor
	if startupHandoffFloor <= 0 {
		startupHandoffFloor = 1500 * time.Millisecond
	}
	sleep := r.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	uiMode := uimode.New(uimode.ModeTranscript)
	stdout = uiMode.BindWriter(uimode.ModeTranscript, stdout)
	stderr = uiMode.BindWriter(uimode.ModeTranscript, stderr)

	if len(args) == 0 {
		return runInteractiveDefault(ctx, interactiveDefaultRunSpec{
			stdin:               stdin,
			stderr:              stderr,
			client:              client,
			attachOrStart:       attachOrStart,
			launchInteractive:   launchInteractive,
			isInteractive:       isInteractive,
			uiMode:              uiMode,
			startupHandoffFloor: startupHandoffFloor,
			sleep:               sleep,
			runner:              r,
		})
	}
	return dispatchSubcommand(ctx, args, start, client, stdout, stderr)
}

type interactiveDefaultRunSpec struct {
	stdin               io.Reader
	stderr              io.Writer
	client              *http.Client
	attachOrStart       func(context.Context, io.Writer, io.Writer, *http.Client) error
	launchInteractive   func(context.Context, io.Reader, io.Writer, io.Writer) error
	isInteractive       func() bool
	uiMode              uiModeBridge
	startupHandoffFloor time.Duration
	sleep               func(time.Duration)
	runner              Runner
}

func runInteractiveDefault(ctx context.Context, spec interactiveDefaultRunSpec) ExitCode {
	if !spec.isInteractive() {
		_, _ = fmt.Fprintln(spec.stderr, "interactive cockpit requires a terminal; use `swobu status` or `swobu daemon --config <path>`")
		return ExitDown
	}
	startupOut := spec.uiMode.BindWriter(uimode.ModeTranscript, spec.runner.Stdout)
	if startupOut == nil {
		startupOut = spec.uiMode.BindWriter(uimode.ModeTranscript, os.Stdout)
	}
	startupErr := spec.uiMode.BindWriter(uimode.ModeTranscript, spec.runner.Stderr)
	if startupErr == nil {
		startupErr = spec.uiMode.BindWriter(uimode.ModeTranscript, os.Stderr)
	}
	versionDecision := emitVersionNoticeIfConfigured(startupOut)
	if versionDecision.show {
		if err := waitForVersionNoticeContinue(spec.stdin, startupOut); err != nil {
			_, _ = fmt.Fprintln(startupErr, err.Error())
			return ExitDown
		}
	}
	if err := ensureTelemetryNoticeBeforeDaemonStart(startupOut); err != nil {
		_, _ = fmt.Fprintln(startupErr, err.Error())
		return ExitDown
	}
	if err := spec.attachOrStart(ctx, startupOut, startupErr, spec.client); err != nil {
		_, _ = fmt.Fprintln(startupErr, err.Error())
		return ExitDown
	}
	spec.sleep(spec.startupHandoffFloor)
	uicli.NewStartupConsolePresenter(startupOut).Emit(uicli.StartupEvent{Kind: uicli.StartupEventHandoffToInteractive})
	if err := spec.uiMode.SetMode(uimode.ModeInteractive); err != nil {
		_, _ = fmt.Fprintln(startupErr, err.Error())
		return ExitDown
	}
	defer func() { _ = spec.uiMode.SetMode(uimode.ModeTranscript) }()
	cockpitOut := spec.uiMode.BindWriter(uimode.ModeInteractive, spec.runner.Stdout)
	if cockpitOut == nil {
		cockpitOut = spec.uiMode.BindWriter(uimode.ModeInteractive, os.Stdout)
	}
	cockpitErr := spec.uiMode.BindWriter(uimode.ModeInteractive, spec.runner.Stderr)
	if cockpitErr == nil {
		cockpitErr = spec.uiMode.BindWriter(uimode.ModeInteractive, os.Stderr)
	}
	prevLogger := slog.Default()
	bridgedLogger := slog.New(platformlogging.NewSessionBufferedHandler(prevLogger.Handler(), spec.uiMode))
	slog.SetDefault(bridgedLogger)
	defer slog.SetDefault(prevLogger)
	if err := spec.launchInteractive(ctx, spec.stdin, cockpitOut, cockpitErr); err != nil {
		_, _ = fmt.Fprintln(cockpitErr, err.Error())
		return ExitDown
	}
	return ExitHealthy
}

func dispatchSubcommand(ctx context.Context, args []string, start func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error), client *http.Client, stdout io.Writer, stderr io.Writer) ExitCode {
	subcommand := args[0] // swobu:io-string source=cli-args
	switch subcommand {
	case "--version", "-v", "version":
		_, _ = fmt.Fprintln(stdout, controlplane.SwobuVersion())
		return ExitHealthy
	case "daemon":
		return runDaemon(ctx, start, stdout, stderr, args[1:])
	case "status":
		return runStatus(ctx, client, stdout, stderr, args[1:])
	case "down":
		return runDown(ctx, client, stdout, stderr, args[1:])
	case "telemetry":
		return runTelemetry(stdout, stderr, args[1:])
	default:
		_, _ = fmt.Fprintf(stderr, "unknown subcommand %q\n", subcommand)
		return ExitDown
	}
}

func runDaemon(ctx context.Context, start func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error), stdout io.Writer, stderr io.Writer, args []string) ExitCode {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: swobu daemon [--config <path>]")
		fs.PrintDefaults()
	}
	configPath := fs.String("config", "", fmt.Sprintf("root daemon config path (env: %s) (default: %s)", platformconfig.EnvConfigPath, platformconfig.DefaultConfigPath()))
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	resolvedConfigPath, err := platformconfig.ResolveDaemonRuntimeConfigPath(*configPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	transcript := uicli.NewStartupConsolePresenter(stdout)
	transcript.Emit(uicli.StartupEvent{Kind: uicli.StartupEventSplash})
	_ = emitVersionNoticeIfConfigured(stdout)
	if err := ensureTelemetryNoticeBeforeDaemonStart(stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	transcript.Emit(uicli.StartupEvent{Kind: uicli.StartupEventDaemonRuntimeStart, ConfigPath: resolvedConfigPath})

	logger := slog.Default()
	daemon, err := start(ctx, bootstrap.StartInput{ConfigPath: resolvedConfigPath, Logger: logger})
	if err != nil {
		next := []string{
			"check daemon config path and values",
			"run `swobu status`",
		}
		if strings.Contains(err.Error(), "bind: address already in use") {
			next = []string{
				"stop existing daemon or run `swobu down`",
				"run `swobu status`",
			}
		}
		transcript.Emit(uicli.StartupEvent{
			Kind:       uicli.StartupEventStartupFailed,
			Text:       err.Error(),
			NextAction: next,
		})
		return ExitDown
	}
	defer func() {
		_ = daemon.Close(context.Background())
		logger.Info("daemon lifecycle", "component", "daemon", "event", "process_stop")
		transcript.Emit(uicli.StartupEvent{Kind: uicli.StartupEventDaemonRuntimeStop})
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

func ensureTelemetryNoticeBeforeDaemonStart(out io.Writer) error {
	store := telemetry.NewStore()
	state, err := store.LoadOrCreate()
	if err != nil {
		return err
	}
	if state.NoticeShown {
		return nil
	}
	uicli.NewStartupConsolePresenter(out).Emit(uicli.StartupEvent{
		Kind: uicli.StartupEventTelemetryDisclosure,
		Text: telemetry.FirstRunNoticeText(),
	})
	_, err = store.MarkNoticeShown()
	return err
}

func runStatus(ctx context.Context, client *http.Client, stdout io.Writer, _ io.Writer, args []string) ExitCode {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stdout, "usage: swobu status [--daemon-url <url>]")
		fs.PrintDefaults()
	}
	daemonURL := fs.String("daemon-url", "", fmt.Sprintf("daemon base URL (env: %s) (default: %s)", platformconfig.EnvDaemonURL, platformconfig.DefaultDaemonURL()))
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}

	payload, exitCode := fetchStatus(ctx, client, platformconfig.ResolveDaemonURL(*daemonURL))
	_ = json.NewEncoder(stdout).Encode(payload)
	return exitCode
}

func runDown(ctx context.Context, client *http.Client, _ io.Writer, stderr io.Writer, args []string) ExitCode {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: swobu down [--daemon-url <url>] [--timeout <duration>]")
		fs.PrintDefaults()
	}
	daemonURL := fs.String("daemon-url", "", fmt.Sprintf("daemon base URL (env: %s) (default: %s)", platformconfig.EnvDaemonURL, platformconfig.DefaultDaemonURL()))
	timeout := fs.Duration("timeout", 5*time.Second, "time to wait for graceful shutdown")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if *timeout <= 0 {
		_, _ = fmt.Fprintln(stderr, "--timeout must be > 0")
		return ExitDown
	}
	result, err := daemonlifecycle.Down(ctx, daemonlifecycle.DownInput{
		DaemonURL: platformconfig.ResolveDaemonURL(*daemonURL),
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

func defaultAttachOrStart(ctx context.Context, stdout io.Writer, _ io.Writer, client *http.Client) error {
	_, err := daemonlifecycle.AttachOrStart(ctx, daemonlifecycle.AttachOrStartInput{
		DaemonURL:            platformconfig.DefaultDaemonURL(),
		Client:               client,
		ResolveDefaultConfig: platformconfig.EnsureDefaultConfigFile,
		Report:               startupReporterFromWriter(stdout),
		ReadinessTimeout:     15 * time.Second,
	})
	return err
}
