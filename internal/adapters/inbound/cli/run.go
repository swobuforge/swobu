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
	"github.com/swobuforge/swobu/internal/producttelemetry"
)

const operatorRequestTimeout = 2 * time.Second
const connectReviewTimeout = 2 * time.Minute

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
	Stdin            io.Reader
	Stdout           io.Writer
	Stderr           io.Writer
	HTTPClient       *http.Client
	Addr             string // explicit local Web Operator address; zero resolves SWOBU_ADDR or the local default
	ConfigPath       string // explicit local Web Operator config path; zero resolves SWOBU_CONFIG_PATH or the platform default
	Start            func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error)
	IsInteractive    func() bool
	AttachOrStart    func(context.Context, io.Writer, io.Writer, *http.Client, string, string) error
	OpenBrowser      func(string) error // injectable OS opener; the URL is a one-use launch capability
	UpdateExecutable func() (string, error)
	RunInstaller     func(context.Context, *http.Client, string, io.Writer, io.Writer) error
}

// Run dispatches explicit commands or opens the authenticated local Web Operator.
// Bare swobu is the same operation as swobu open, with the root flags applied.
func (r Runner) Run(ctx context.Context, args []string) ExitCode {
	if r.Stdin == nil {
		r.Stdin = os.Stdin
	}
	if r.Stdout == nil {
		r.Stdout = os.Stdout
	}
	if r.Stderr == nil {
		r.Stderr = os.Stderr
	}
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: operatorRequestTimeout}
	}
	start := r.Start
	if start == nil {
		start = bootstrap.Start
	}
	if len(args) != 0 {
		return dispatchSubcommand(ctx, args, start, client, r.Stdout, r.Stderr, r)
	}
	return runOpen(ctx, client, r.Stdout, r.Stderr, nil, r)
}

func dispatchSubcommand(ctx context.Context, args []string, start func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error), client *http.Client, stdout io.Writer, stderr io.Writer, runner Runner) ExitCode {
	subcommand := args[0] // swobu:io-string source=cli-args
	switch subcommand {
	case "open":
		return runOpen(ctx, client, stdout, stderr, args[1:], runner)
	case "--version", "-v", "version":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "version takes no arguments")
			return ExitDown
		}
		_, _ = fmt.Fprintln(stdout, controlplane.SwobuVersion())
		return ExitHealthy
	case "config":
		return runConfig(ctx, stdout, stderr, args[1:])
	case "daemon":
		if len(args) > 1 && args[1] == "down" {
			return runDown(ctx, client, stdout, stderr, args[2:], runner.Addr)
		}
		return runDaemon(ctx, start, stdout, stderr, args[1:], runner.Addr, runner.ConfigPath)
	case "status":
		return runStatus(ctx, client, stdout, stderr, args[1:], runner.Addr)
	case "telemetry":
		return runTelemetry(ctx, client, stdout, stderr, args[1:], runner.Addr)
	case "update":
		return runUpdate(ctx, stdout, stderr, args[1:], runner)
	case "connect":
		return runConnect(ctx, withClientTimeout(client, connectReviewTimeout), stdout, stderr, args[1:], runner)
	case "credential":
		return runCredential(ctx, client, stdout, stderr, args[1:], runner)
	case "principal":
		return runPrincipal(ctx, client, stdout, stderr, args[1:], runner)
	case "workspace":
		if len(args) > 1 && args[1] == "limits" {
			return runWorkspaceLimits(ctx, client, stdout, stderr, args[2:], runner)
		}
		if len(args) > 1 && args[1] == "create" {
			return runWorkspaceCreate(ctx, client, stdout, stderr, args[2:], runner)
		}
		if len(args) > 1 && args[1] == "display-name" {
			return runWorkspaceDisplayName(ctx, client, stdout, stderr, args[2:], runner)
		}
		if len(args) > 1 && args[1] == "permission" {
			return runWorkspacePermission(ctx, client, stdout, stderr, args[2:], runner)
		}
		if len(args) > 1 && args[1] == "delete" {
			return runWorkspaceDelete(ctx, client, stdout, stderr, args[2:], runner)
		}
		fmt.Fprintln(stderr, "usage: swobu workspace create|display-name|delete|permission|limits [args]")
		return ExitDown
	default:
		_, _ = fmt.Fprintf(stderr, "unknown subcommand %q\n", subcommand)
		return ExitDown
	}
}

func withClientTimeout(client *http.Client, timeout time.Duration) *http.Client {
	copy := *client
	if copy.Timeout > 0 && copy.Timeout < timeout {
		copy.Timeout = timeout
	}
	return &copy
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

func runDaemon(ctx context.Context, start func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error), stdout io.Writer, stderr io.Writer, args []string, defaultAddr, defaultConfigPath string) ExitCode {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: swobu daemon [--config <path>] [--addr <host:port>] [--config-read-only]")
		fs.PrintDefaults()
		_, _ = fmt.Fprintln(stderr, "\ncommands:\n  down    Request daemon shutdown")
	}
	configPath := fs.String("config", defaultConfigPath, fmt.Sprintf("root daemon config path (env: %s) (default: %s)", platformconfig.EnvConfigPath, platformconfig.DefaultConfigPath()))
	addr := fs.String("addr", defaultAddr, fmt.Sprintf("address (env: %s) (default: %s)", platformconfig.EnvAddr, platformconfig.DefaultAddr()))
	configReadOnly := fs.Bool("config-read-only", false, "run with existing externally managed Node YAML; refuse configuration writes")
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
	daemon, err := start(ctx, bootstrap.StartInput{ConfigPath: resolvedConfigPath, ReadOnlyConfig: *configReadOnly, StartupConfig: startupConfig, Logger: logger})
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

func runStatus(ctx context.Context, client *http.Client, stdout io.Writer, stderr io.Writer, args []string, defaultAddr string) ExitCode {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stdout, "usage: swobu status [--addr <host:port>]")
		fs.PrintDefaults()
	}
	addr := fs.String("addr", defaultAddr, fmt.Sprintf("address (env: %s) (default: %s)", platformconfig.EnvAddr, platformconfig.DefaultAddr()))
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
		fmt.Fprintln(stderr, err)
		return ExitDown
	}
	payload, exitCode := fetchStatus(ctx, client, startupConfig.Addr)
	_ = json.NewEncoder(stdout).Encode(payload)
	return exitCode
}

func runDown(ctx context.Context, client *http.Client, _ io.Writer, stderr io.Writer, args []string, defaultAddr string) ExitCode {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: swobu daemon down [--addr <host:port>] [--timeout <duration>]")
		fs.PrintDefaults()
	}
	addr := fs.String("addr", defaultAddr, fmt.Sprintf("address (env: %s) (default: %s)", platformconfig.EnvAddr, platformconfig.DefaultAddr()))
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
