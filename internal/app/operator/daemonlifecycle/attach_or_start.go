package daemonlifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

type StatusPayload struct {
	State                string `json:"state"`
	WorkspaceCount       int    `json:"workspace_count,omitempty"`
	ControlPlaneProtocol *int   `json:"control_plane_protocol,omitempty"`
	SwobuVersion         string `json:"swobu_version,omitempty"`
}

type StatusClass string

const (
	StatusClassHealthy       StatusClass = "healthy"
	StatusClassUninitialized StatusClass = "uninitialized"
	StatusClassDegraded      StatusClass = "degraded"
	StatusClassDown          StatusClass = "down"
)

type StartupEventKind string

const (
	StartupEventSplash             StartupEventKind = "splash"
	StartupEventDaemonReady        StartupEventKind = "daemon_ready"
	StartupEventDaemonNotReachable StartupEventKind = "daemon_not_reachable"
	StartupEventStartingDaemon     StartupEventKind = "starting_daemon"
	StartupEventWaitingReadiness   StartupEventKind = "waiting_readiness"
	StartupEventStartupFailed      StartupEventKind = "startup_failed"
	StartupEventStartupTimedOut    StartupEventKind = "startup_timed_out"
)

type StartupEvent struct {
	Kind       StartupEventKind
	State      string
	Addr       string
	Text       string
	NextAction []string
}

type StartupReporter interface {
	Report(StartupEvent)
}

type startupReporterFunc func(StartupEvent)

func (f startupReporterFunc) Report(ev StartupEvent) {
	if f == nil {
		return
	}
	f(ev)
}

type AttachOrStartInput struct {
	Addr                  string
	Client                *http.Client
	ReadinessTimeout      time.Duration
	ResolveConfigPath     func() string
	SpawnForegroundDaemon func(ctx context.Context, configPath, addr string) (<-chan error, error)
	Report                StartupReporter
}

type DownInput struct {
	Addr    string
	Client  *http.Client
	Timeout time.Duration
}

type RestartInput struct {
	Addr                  string
	Client                *http.Client
	ReadinessTimeout      time.Duration
	ResolveConfigPath     func() string
	SpawnForegroundDaemon func(ctx context.Context, configPath, addr string) (<-chan error, error)
	Report                StartupReporter
}

type DownResult string

const (
	DownResultAlreadyStopped DownResult = "already_stopped"
	DownResultStopped        DownResult = "stopped"
)

func FetchStatus(ctx context.Context, client *http.Client, addr string) (StatusPayload, StatusClass) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, platformconfig.BaseURL(addr)+"/_swobu/status", nil)
	if err != nil {
		return StatusPayload{State: string(StatusClassDown)}, StatusClassDown
	}
	resp, err := client.Do(req)
	if err != nil {
		return StatusPayload{State: string(StatusClassDown)}, StatusClassDown
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return StatusPayload{State: string(StatusClassDown)}, StatusClassDown
	}
	var payload StatusPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return StatusPayload{State: string(StatusClassDown)}, StatusClassDown
	}
	state := payload.State // swobu:io-string source=http
	switch state {
	case "healthy":
		return payload, StatusClassHealthy
	case "uninitialized":
		return payload, StatusClassUninitialized
	case "degraded":
		return payload, StatusClassDegraded
	default:
		return StatusPayload{State: string(StatusClassDown)}, StatusClassDown
	}
}

func AttachOrStart(ctx context.Context, in AttachOrStartInput) (StatusPayload, error) {
	client := in.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	addr := in.Addr
	if addr == "" {
		return StatusPayload{}, errors.New("daemon address is required")
	}
	report := in.Report
	if report == nil {
		report = startupReporterFunc(nil)
	}
	report.Report(StartupEvent{Kind: StartupEventSplash})

	payload, class := FetchStatus(ctx, client, addr)
	if class != StatusClassDown {
		report.Report(StartupEvent{Kind: StartupEventDaemonReady, State: payload.State})
		return payload, nil
	}
	report.Report(StartupEvent{Kind: StartupEventDaemonNotReachable, Addr: addr})

	resolveConfig := in.ResolveConfigPath
	if resolveConfig == nil {
		return StatusPayload{}, errors.New("resolve daemon config function is required")
	}
	configPath := resolveConfig()

	spawn := in.SpawnForegroundDaemon
	if spawn == nil {
		spawn = defaultSpawnForegroundDaemon
	}
	daemonExited, err := spawn(ctx, configPath, addr)
	if err != nil {
		report.Report(StartupEvent{
			Kind:       StartupEventStartupFailed,
			Text:       fmt.Sprintf("start daemon: %v", err),
			NextAction: startupFailureActions(addr, configPath),
		})
		return StatusPayload{}, fmt.Errorf("start daemon: %w", err)
	}
	report.Report(StartupEvent{Kind: StartupEventStartingDaemon})
	report.Report(StartupEvent{Kind: StartupEventWaitingReadiness})

	timeout := in.ReadinessTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	readinessCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	status, err := waitForDaemonReadiness(readinessCtx, client, addr, daemonExited)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			report.Report(StartupEvent{
				Kind:       StartupEventStartupTimedOut,
				Text:       "daemon readiness timed out",
				NextAction: startupFailureActions(addr, configPath),
			})
		} else {
			report.Report(StartupEvent{
				Kind:       StartupEventStartupFailed,
				Text:       fmt.Sprintf("daemon exited before readiness: %v", err),
				NextAction: startupFailureActions(addr, configPath),
			})
			return StatusPayload{}, fmt.Errorf("daemon startup failed: %w", err)
		}
		return StatusPayload{}, fmt.Errorf("daemon readiness failed (check `swobu status` and foreground daemon diagnostics): %w", err)
	}
	report.Report(StartupEvent{Kind: StartupEventDaemonReady, State: status.State})
	return status, nil
}

func Down(ctx context.Context, in DownInput) (DownResult, error) {
	client := in.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	addr := in.Addr
	if addr == "" {
		return "", errors.New("daemon address is required")
	}
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if _, class := FetchStatus(ctx, client, addr); class == StatusClassDown {
		return DownResultAlreadyStopped, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, platformconfig.BaseURL(addr)+"/_swobu/down", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		if _, class := FetchStatus(ctx, client, addr); class == StatusClassDown {
			return DownResultAlreadyStopped, nil
		}
		return "", err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("shutdown failed with status %d", resp.StatusCode)
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, class := FetchStatus(waitCtx, client, addr); class == StatusClassDown {
			return DownResultStopped, nil
		}
		select {
		case <-waitCtx.Done():
			return "", fmt.Errorf("shutdown timed out")
		case <-ticker.C:
		}
	}
}

func Restart(ctx context.Context, in RestartInput) error {
	client := in.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	addr := in.Addr
	if addr == "" {
		return errors.New("daemon address is required")
	}
	if _, err := Down(ctx, DownInput{
		Addr:    addr,
		Client:  client,
		Timeout: 5 * time.Second,
	}); err != nil {
		return err
	}
	_, err := AttachOrStart(ctx, AttachOrStartInput{
		Addr:                  addr,
		Client:                client,
		ReadinessTimeout:      in.ReadinessTimeout,
		ResolveConfigPath:     in.ResolveConfigPath,
		SpawnForegroundDaemon: in.SpawnForegroundDaemon,
		Report:                in.Report,
	})
	return err
}

func waitForDaemonReadiness(ctx context.Context, client *http.Client, addr string, daemonExited <-chan error) (StatusPayload, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-daemonExited:
			return StatusPayload{}, daemonExitError(err)
		default:
		}
		payload, class := FetchStatus(ctx, client, addr)
		if class != StatusClassDown && isReadinessState(payload.State) {
			return payload, nil
		}
		select {
		case <-ctx.Done():
			return StatusPayload{}, ctx.Err()
		case err := <-daemonExited:
			return StatusPayload{}, daemonExitError(err)
		case <-ticker.C:
		}
	}
}

func daemonExitError(err error) error {
	if err == nil {
		return errors.New("daemon process stopped")
	}
	return fmt.Errorf("daemon process stopped: %w", err)
}

func startupFailureActions(addr, configPath string) []string {
	return []string{
		fmt.Sprintf("if another daemon owns %q, stop it using its current address", configPath),
		fmt.Sprintf("run `swobu daemon --addr %s --config %q` for foreground diagnostics", addr, configPath),
		fmt.Sprintf("run `swobu status --addr %s`", addr),
	}
}

func isReadinessState(state string) bool {
	readiness := state // swobu:io-string source=http
	switch readiness {
	case "healthy", "uninitialized", "degraded":
		return true
	default:
		return false
	}
}

func defaultSpawnForegroundDaemon(_ context.Context, configPath, addr string) (<-chan error, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve swobu executable: %w", err)
	}
	command := exec.Command(executablePath, "daemon", "--config", configPath, "--addr", addr)
	if err := command.Start(); err != nil {
		return nil, err
	}
	// Start confirms only that the OS created the process. Wait owns process
	// reclamation and lets AttachOrStart distinguish an early child failure from
	// an HTTP readiness timeout.
	exited := make(chan error, 1)
	go func() {
		exited <- command.Wait()
		close(exited)
	}()
	return exited, nil
}
