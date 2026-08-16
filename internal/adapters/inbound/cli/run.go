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
	"github.com/swobuforge/swobu/internal/cockpit"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	platformlogging "github.com/swobuforge/swobu/internal/platform/logging"
	"github.com/swobuforge/swobu/internal/producttelemetry"
	"golang.org/x/term"
)

// ExitCode is contract-bearing for `swobu status`: healthy=0, uninitialized=1, daemon unreachable=2.
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
	Addr                string // explicit interactive launcher address; zero resolves SWOBU_ADDR or the local default
	ConfigPath          string // explicit interactive launcher config path; zero resolves SWOBU_CONFIG_PATH or the platform default
	Start               func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error)
	IsInteractive       func() bool
	AttachOrStart       func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error
	LaunchInteractive   func(context.Context, io.Reader, io.Writer, io.Writer, string) error
	StartupHandoffFloor time.Duration
	Sleep               func(time.Duration)
	ConnectOperations   connectOperations
	ConnectAttach       func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error
	ConnectWorkspaces   connectWorkspaceLister
}

// daemon control, explicit lifecycle commands, and go-tui launch handoff.
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
	if len(args) != 0 {
		return dispatchSubcommand(ctx, args, start, client, stdout, stderr, r)
	}
	startupConfig, err := platformconfig.ResolveStartupConfig(r.Addr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	configPath := platformconfig.ResolveConfigPath(r.ConfigPath)
	isInteractive := r.IsInteractive
	if isInteractive == nil {
		isInteractive = defaultIsInteractive
	}
	launchInteractive := r.LaunchInteractive
	if launchInteractive == nil {
		// V0: direct launch into the active go-tui Cockpit.
		// We import internal/cockpit — the canonical operator TUI authority —
		// because it owns the interactive workspace surface.
		launchInteractive = func(ctx context.Context, stdin io.Reader, stdout io.Writer, stderr io.Writer, addr string) error {
			return cockpit.Run(ctx, addr, stdin, stdout, stderr)
		}
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

	return runInteractiveDefault(ctx, interactiveDefaultRunSpec{
		stdin:               stdin,
		stdout:              stdout,
		stderr:              stderr,
		client:              client,
		addr:                startupConfig.Addr,
		configPath:          configPath,
		attachOrStart:       attachOrStart,
		launchInteractive:   launchInteractive,
		isInteractive:       isInteractive,
		startupHandoffFloor: startupHandoffFloor,
		sleep:               sleep,
	})
}

type interactiveDefaultRunSpec struct {
	stdin               io.Reader
	stdout              io.Writer
	stderr              io.Writer
	client              *http.Client
	addr                string
	configPath          string
	attachOrStart       func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error
	launchInteractive   func(context.Context, io.Reader, io.Writer, io.Writer, string) error
	isInteractive       func() bool
	startupHandoffFloor time.Duration
	sleep               func(time.Duration)
}

func runInteractiveDefault(ctx context.Context, spec interactiveDefaultRunSpec) ExitCode {
	if !spec.isInteractive() {
		_, _ = fmt.Fprintln(spec.stderr, "interactive cockpit requires a terminal; use `swobu status` or `swobu daemon --config <path>`")
		return ExitDown
	}
	startupOut := spec.stdout
	startupErr := spec.stderr
	startupReporterFromWriter(startupOut).Report(daemonlifecycle.StartupEvent{Kind: daemonlifecycle.StartupEventSplash})
	versionDecision := emitVersionNoticeIfConfigured(startupOut)
	if versionDecision.show {
		if err := waitForVersionNoticeContinue(spec.stdin, startupOut); err != nil {
			if !errors.Is(err, errVersionNoticeAcknowledgmentUnavailable) {
				_, _ = fmt.Fprintln(startupErr, err.Error())
				return ExitDown
			}
		}
	}
	if err := ensureTelemetryNoticeBeforeDaemonStart(startupOut); err != nil {
		_, _ = fmt.Fprintln(startupErr, err.Error())
		return ExitDown
	}
	if err := spec.attachOrStart(ctx, startupOut, startupErr, spec.client, spec.addr, spec.configPath); err != nil {
		_, _ = fmt.Fprintln(startupErr, err.Error())
		return ExitDown
	}
	spec.sleep(spec.startupHandoffFloor)
	debugHandoff := platformconfig.EnvTruthy(os.Getenv("SWOBU_E2E_DEBUG_HANDOFF"))
	if debugHandoff {
		_, _ = fmt.Fprintln(startupOut, "swobu handoff: starting interactive cockpit")
		_, _ = fmt.Fprintln(startupErr, "swobu handoff: starting interactive cockpit")
	}
	clearInteractiveScreen(spec.stdout)
	prevLogger := slog.Default()
	bufferedHandler := platformlogging.NewBufferedHandler(prevLogger.Handler())
	slog.SetDefault(slog.New(bufferedHandler))
	defer slog.SetDefault(prevLogger)
	if debugHandoff {
		_, _ = fmt.Fprintln(startupOut, "swobu handoff: launching cockpit interactive app")
		_, _ = fmt.Fprintln(startupErr, "swobu handoff: launching cockpit interactive app")
	}
	if err := spec.launchInteractive(ctx, spec.stdin, spec.stdout, spec.stderr, spec.addr); err != nil {
		bufferedHandler.Flush(context.Background())
		// Mirror launch failure into stderr; if the cockpit failed before drawing,
		// operators would otherwise only see the handoff failure indirectly.
		_, _ = fmt.Fprintln(startupErr, err.Error())
		return ExitDown
	}
	bufferedHandler.Flush(context.Background())
	if debugHandoff {
		_, _ = fmt.Fprintln(startupOut, "swobu handoff: cockpit interactive app exited cleanly")
		_, _ = fmt.Fprintln(startupErr, "swobu handoff: cockpit interactive app exited cleanly")
	}
	return ExitHealthy
}

func clearInteractiveScreen(out io.Writer) {
	file, ok := out.(*os.File)
	if !ok || file == nil || !term.IsTerminal(int(file.Fd())) {
		return
	}
	_, _ = out.Write([]byte("\x1b[2J\x1b[H"))
}

func dispatchSubcommand(ctx context.Context, args []string, start func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error), client *http.Client, stdout io.Writer, stderr io.Writer, runner Runner) ExitCode {
	subcommand := args[0] // swobu:io-string source=cli-args
	switch subcommand {
	case "--version", "-v", "version":
		_, _ = fmt.Fprintln(stdout, controlplane.SwobuVersion())
		return ExitHealthy
	case "daemon":
		if len(args) > 1 && args[1] == "down" {
			return runDown(ctx, client, stdout, stderr, args[2:])
		}
		return runDaemon(ctx, start, stdout, stderr, args[1:])
	case "status":
		return runStatus(ctx, client, stdout, stderr, args[1:])
	case "telemetry":
		return runTelemetry(stdout, stderr, args[1:])
	case "connect":
		return runConnect(ctx, client, stdout, stderr, args[1:], runner)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown subcommand %q\n", subcommand)
		return ExitDown
	}
}

// rejectUnexpectedPositionalArgs reports whether fs still holds positional
// arguments after flag parsing. Leaf commands take only flags, so a leftover
// positional input is a user error, not a silent no-op.
// When it rejects, it prints one usage-style line to out and the caller returns
// ExitDown.
func rejectUnexpectedPositionalArgs(fs *flag.FlagSet, out io.Writer) bool {
	if fs.NArg() == 0 {
		return false
	}
	fmt.Fprintf(out, "unexpected argument %q\n", fs.Arg(0))
	return true
}

func runDaemon(ctx context.Context, start func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error), stdout io.Writer, stderr io.Writer, args []string) ExitCode {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: swobu daemon [--config <path>] [--addr <host:port>]")
		fs.PrintDefaults()
		_, _ = fmt.Fprintln(stderr, "\ncommands:\n  down    Request daemon shutdown")
	}
	configPath := fs.String("config", "", fmt.Sprintf("root daemon config path (env: %s) (default: %s)", platformconfig.EnvConfigPath, platformconfig.DefaultConfigPath()))
	addr := fs.String("addr", "", fmt.Sprintf("address (env: %s) (default: %s)", platformconfig.EnvAddr, platformconfig.DefaultAddr()))
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) {
		return ExitDown
	}
	resolvedConfigPath := platformconfig.ResolveConfigPath(*configPath)
	startupConfig, err := platformconfig.ResolveStartupConfig(*addr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	startupReporter := startupReporterFromWriter(stdout)
	startupReporter.Report(daemonlifecycle.StartupEvent{Kind: daemonlifecycle.StartupEventSplash})
	_ = emitVersionNoticeIfConfigured(stdout)
	if err := ensureTelemetryNoticeBeforeDaemonStart(stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	writeStartupLine(stdout, "starting daemon runtime")
	writeNoticeBlock(stdout, "Daemon Runtime", []string{
		"config path: " + resolvedConfigPath,
		"address: " + startupConfig.Addr,
	})

	logger := slog.Default()
	daemon, err := start(ctx, bootstrap.StartInput{ConfigPath: resolvedConfigPath, StartupConfig: startupConfig, Logger: logger})
	if err != nil {
		next := []string{
			"check daemon config path and values",
			"run `swobu status`",
		}
		if strings.Contains(err.Error(), "bind: address already in use") {
			next = []string{
				"stop existing daemon or run `swobu daemon down`",
				"run `swobu status`",
			}
		}
		startupReporter.Report(daemonlifecycle.StartupEvent{
			Kind:       daemonlifecycle.StartupEventStartupFailed,
			Text:       err.Error(),
			NextAction: next,
		})
		return ExitDown
	}
	defer func() {
		_ = daemon.Close()
		logger.Info("daemon lifecycle", "component", "daemon", "event", "process_stop")
	}()

	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-signalCtx.Done():
		// The deferred daemon.Close() performs graceful shutdown on the single
		// exit path; do not close twice or wait here.
		logger.Info("daemon lifecycle", "component", "daemon", "event", "signal_received", "signal", "interrupt_or_sigterm")
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
	// The claim is the single atomic create-if-absent, so a concurrent process
	// cannot also print. The notice prints only when this process won the claim;
	// a store failure is best-effort and never blocks daemon start.
	claimed, err := producttelemetry.ClaimNotice()
	if err == nil && claimed && out != nil {
		_, _ = fmt.Fprintln(out, producttelemetry.FirstRunNoticeText())
	}
	return nil
}

func runStatus(ctx context.Context, client *http.Client, stdout io.Writer, _ io.Writer, args []string) ExitCode {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stdout, "usage: swobu status [--addr <host:port>]")
		fs.PrintDefaults()
	}
	addr := fs.String("addr", "", fmt.Sprintf("address (env: %s) (default: %s)", platformconfig.EnvAddr, platformconfig.DefaultAddr()))
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stdout) {
		return ExitDown
	}

	startupConfig, err := platformconfig.ResolveStartupConfig(*addr)
	if err != nil {
		return ExitDown
	}
	payload, exitCode := fetchStatus(ctx, client, startupConfig.Addr)
	_ = json.NewEncoder(stdout).Encode(payload)
	return exitCode
}

func runDown(ctx context.Context, client *http.Client, _ io.Writer, stderr io.Writer, args []string) ExitCode {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: swobu daemon down [--addr <host:port>] [--timeout <duration>]")
		fs.PrintDefaults()
	}
	addr := fs.String("addr", "", fmt.Sprintf("address (env: %s) (default: %s)", platformconfig.EnvAddr, platformconfig.DefaultAddr()))
	timeout := fs.Duration("timeout", 5*time.Second, "time to wait for graceful shutdown")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitHealthy
		}
		return ExitDown
	}
	if rejectUnexpectedPositionalArgs(fs, stderr) {
		return ExitDown
	}
	if *timeout <= 0 {
		_, _ = fmt.Fprintln(stderr, "--timeout must be > 0")
		return ExitDown
	}
	startupConfig, err := platformconfig.ResolveStartupConfig(*addr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return ExitDown
	}
	result, err := daemonlifecycle.Down(ctx, daemonlifecycle.DownInput{
		Addr:    startupConfig.Addr,
		Client:  client,
		Timeout: *timeout,
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

func fetchStatus(ctx context.Context, client *http.Client, addr string) (StatusPayload, ExitCode) {
	payload, class := daemonlifecycle.FetchStatus(ctx, client, addr)
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
func defaultAttachOrStart(ctx context.Context, stdout io.Writer, _ io.Writer, client *http.Client, addr, configPath string) error {
	_, err := daemonlifecycle.AttachOrStart(ctx, daemonlifecycle.AttachOrStartInput{
		Addr:              addr,
		Client:            client,
		ResolveConfigPath: func() string { return configPath },
		Report:            withoutStartupSplash(startupReporterFromWriter(stdout)),
		ReadinessTimeout:  15 * time.Second,
	})
	return err
}
