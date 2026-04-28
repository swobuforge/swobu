package daemonlifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/metrofun/swobu/internal/bootstrap"
	platformconfig "github.com/metrofun/swobu/internal/platform/config"
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

type AttachOrStartInput struct {
	DaemonURL             string
	Client                *http.Client
	Stdout                io.Writer
	ReadinessTimeout      time.Duration
	ResolveDefaultConfig  func() (string, error)
	OpenDaemonLogSink     func() (string, *os.File, error)
	SpawnForegroundDaemon func(ctx context.Context, configPath string, sink *os.File) error
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
	OpenDaemonLogSink     func() (string, *os.File, error)
	SpawnForegroundDaemon func(ctx context.Context, configPath string, sink *os.File) error
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
	switch payload.State {
	case string(bootstrap.HealthStateHealthy):
		return payload, StatusClassHealthy
	case string(bootstrap.HealthStateUninitialized):
		return payload, StatusClassUninitialized
	case string(bootstrap.HealthStateDegraded):
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
		daemonURL = platformconfig.DefaultDaemonURL()
	}
	stdout := in.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	payload, class := FetchStatus(ctx, client, daemonURL)
	if class != StatusClassDown {
		return payload, nil
	}
	_, _ = fmt.Fprintf(stdout, "daemon not reachable at %s\n", daemonURL)

	resolveConfig := in.ResolveDefaultConfig
	if resolveConfig == nil {
		resolveConfig = platformconfig.EnsureDefaultConfigFile
	}
	configPath, err := resolveConfig()
	if err != nil {
		return StatusPayload{}, fmt.Errorf("resolve daemon config: %w", err)
	}

	openSink := in.OpenDaemonLogSink
	if openSink == nil {
		openSink = openDaemonLogSink
	}
	logPath, sink, err := openSink()
	if err != nil {
		return StatusPayload{}, fmt.Errorf("open daemon log sink: %w", err)
	}
	spawn := in.SpawnForegroundDaemon
	if spawn == nil {
		spawn = defaultSpawnForegroundDaemon
	}
	if err := spawn(ctx, configPath, sink); err != nil {
		_ = sink.Close()
		return StatusPayload{}, fmt.Errorf("start daemon: %w", err)
	}
	_ = sink.Close()
	_, _ = fmt.Fprintln(stdout, "starting daemon")
	_, _ = fmt.Fprintln(stdout, "waiting for daemon readiness")

	timeout := in.ReadinessTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	readinessCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	status, err := waitForDaemonReadiness(readinessCtx, client, daemonURL)
	if err != nil {
		return StatusPayload{}, fmt.Errorf("daemon readiness failed (check `swobu status` and logs at %s): %w", logPath, err)
	}
	_, _ = fmt.Fprintf(stdout, "daemon ready (%s)\n", status.State)
	return status, nil
}

func Down(ctx context.Context, in DownInput) (DownResult, error) {
	client := in.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	daemonURL := in.DaemonURL
	if daemonURL == "" {
		daemonURL = platformconfig.DefaultDaemonURL()
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
		daemonURL = platformconfig.DefaultDaemonURL()
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
		Stdout:                io.Discard,
		ReadinessTimeout:      in.ReadinessTimeout,
		ResolveDefaultConfig:  in.ResolveDefaultConfig,
		OpenDaemonLogSink:     in.OpenDaemonLogSink,
		SpawnForegroundDaemon: in.SpawnForegroundDaemon,
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
	switch state {
	case string(bootstrap.HealthStateHealthy),
		string(bootstrap.HealthStateUninitialized),
		string(bootstrap.HealthStateDegraded):
		return true
	default:
		return false
	}
}

func defaultSpawnForegroundDaemon(_ context.Context, configPath string, sink *os.File) error {
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve swobu executable: %w", err)
	}
	command := exec.Command(executablePath, "daemon", "--config", configPath)
	command.Stdout = sink
	command.Stderr = sink
	return command.Start()
}
