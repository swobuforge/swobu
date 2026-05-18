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
)

type StatusPayload struct {
	State                string `json:"state"`
	EndpointCount        int    `json:"endpoint_count,omitempty"`
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
	DaemonURL  string
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
	DaemonURL             string
	Client                *http.Client
	ReadinessTimeout      time.Duration
	ResolveDefaultConfig  func() (string, error)
	SpawnForegroundDaemon func(ctx context.Context, configPath string) error
	Report                StartupReporter
}

type DownInput struct {
	DaemonURL string
	Client    *http.Client
	Timeout   time.Duration
}

type RestartInput struct {
	DaemonURL             string
	Client                *http.Client
	ReadinessTimeout      time.Duration
	ResolveDefaultConfig  func() (string, error)
	SpawnForegroundDaemon func(ctx context.Context, configPath string) error
	Report                StartupReporter
}

type DownResult string

const (
	DownResultAlreadyStopped DownResult = "already_stopped"
	DownResultStopped        DownResult = "stopped"
)

func FetchStatus(ctx context.Context, client *http.Client, daemonURL string) (StatusPayload, StatusClass) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, daemonURL+"/_swobu/status", nil)
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
	daemonURL := in.DaemonURL
	if daemonURL == "" {
		return StatusPayload{}, errors.New("daemon URL is required")
	}
	report := in.Report
	if report == nil {
		report = startupReporterFunc(nil)
	}
	report.Report(StartupEvent{Kind: StartupEventSplash})

	payload, class := FetchStatus(ctx, client, daemonURL)
	if class != StatusClassDown {
		report.Report(StartupEvent{Kind: StartupEventDaemonReady, State: payload.State})
		return payload, nil
	}
	report.Report(StartupEvent{Kind: StartupEventDaemonNotReachable, DaemonURL: daemonURL})

	resolveConfig := in.ResolveDefaultConfig
	if resolveConfig == nil {
		return StatusPayload{}, errors.New("resolve daemon config function is required")
	}
	configPath, err := resolveConfig()
	if err != nil {
		report.Report(StartupEvent{
			Kind: StartupEventStartupFailed,
			Text: fmt.Sprintf("resolve daemon config: %v", err),
			NextAction: []string{
				"check local config path and permissions",
				"run `swobu status`",
			},
		})
		return StatusPayload{}, fmt.Errorf("resolve daemon config: %w", err)
	}

	spawn := in.SpawnForegroundDaemon
	if spawn == nil {
		spawn = defaultSpawnForegroundDaemon
	}
	if err := spawn(ctx, configPath); err != nil {
		report.Report(StartupEvent{
			Kind: StartupEventStartupFailed,
			Text: fmt.Sprintf("start daemon: %v", err),
			NextAction: []string{
				"run `swobu daemon --config <path>` for foreground diagnostics",
				"run `swobu status`",
			},
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
	status, err := waitForDaemonReadiness(readinessCtx, client, daemonURL)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			report.Report(StartupEvent{
				Kind: StartupEventStartupTimedOut,
				Text: "daemon readiness timed out",
				NextAction: []string{
					"run `swobu status`",
					"run `swobu daemon --config <path>` for foreground diagnostics",
				},
			})
		} else {
			report.Report(StartupEvent{
				Kind: StartupEventStartupFailed,
				Text: fmt.Sprintf("daemon readiness failed: %v", err),
				NextAction: []string{
					"run `swobu status`",
					"run `swobu daemon --config <path>` for foreground diagnostics",
				},
			})
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
	daemonURL := in.DaemonURL
	if daemonURL == "" {
		return "", errors.New("daemon URL is required")
	}
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if _, class := FetchStatus(ctx, client, daemonURL); class == StatusClassDown {
		return DownResultAlreadyStopped, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, daemonURL+"/_swobu/down", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		if _, class := FetchStatus(ctx, client, daemonURL); class == StatusClassDown {
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
		if _, class := FetchStatus(waitCtx, client, daemonURL); class == StatusClassDown {
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
	daemonURL := in.DaemonURL
	if daemonURL == "" {
		return errors.New("daemon URL is required")
	}
	if _, err := Down(ctx, DownInput{
		DaemonURL: daemonURL,
		Client:    client,
		Timeout:   5 * time.Second,
	}); err != nil {
		return err
	}
	_, err := AttachOrStart(ctx, AttachOrStartInput{
		DaemonURL:             daemonURL,
		Client:                client,
		ReadinessTimeout:      in.ReadinessTimeout,
		ResolveDefaultConfig:  in.ResolveDefaultConfig,
		SpawnForegroundDaemon: in.SpawnForegroundDaemon,
		Report:                in.Report,
	})
	return err
}

func waitForDaemonReadiness(ctx context.Context, client *http.Client, daemonURL string) (StatusPayload, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		payload, class := FetchStatus(ctx, client, daemonURL)
		if class != StatusClassDown && isReadinessState(payload.State) {
			return payload, nil
		}
		select {
		case <-ctx.Done():
			return StatusPayload{}, ctx.Err()
		case <-ticker.C:
		}
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

func defaultSpawnForegroundDaemon(_ context.Context, configPath string) error {
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve swobu executable: %w", err)
	}
	command := exec.Command(executablePath, "daemon", "--config", configPath)
	return command.Start()
}
