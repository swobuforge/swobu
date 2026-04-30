package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestRunner_TelemetryCommand_UltraLeanFlow(t *testing.T) {
	t.Setenv("SWOBU_TELEMETRY_STATE_PATH", t.TempDir()+"/telemetry/state.json")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	exitCode := runner.Run(context.Background(), []string{"telemetry", "status"})
	if exitCode != ExitHealthy {
		t.Fatalf("status exit code = %d, want %d, stderr=%s", exitCode, ExitHealthy, stderr.String())
	}
	var statusPayload struct {
		Enabled    bool `json:"enabled"`
		DoNotTrack bool `json:"do_not_track"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &statusPayload); err != nil {
		t.Fatalf("status output is not JSON: %v; raw=%q", err, stdout.String())
	}
	if !statusPayload.Enabled {
		t.Fatal("status enabled = false, want true")
	}
	if statusPayload.DoNotTrack {
		t.Fatal("status do_not_track = true, want false")
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = runner.Run(context.Background(), []string{"telemetry", "off"})
	if exitCode != ExitHealthy {
		t.Fatalf("off exit code = %d, want %d, stderr=%s", exitCode, ExitHealthy, stderr.String())
	}
	var togglePayload struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &togglePayload); err != nil {
		t.Fatalf("off output is not JSON: %v; raw=%q", err, stdout.String())
	}
	if togglePayload.Enabled {
		t.Fatal("off enabled = true, want false")
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = runner.Run(context.Background(), []string{"telemetry", "log"})
	if exitCode != ExitDown {
		t.Fatalf("log exit code = %d, want %d", exitCode, ExitDown)
	}
	if got := stderr.String(); got == "" {
		t.Fatal("stderr empty for removed telemetry log subcommand")
	}
}

func TestRunner_TelemetryCommand_DoNotTrackOverride(t *testing.T) {
	t.Setenv("SWOBU_TELEMETRY_STATE_PATH", t.TempDir()+"/telemetry/state.json")
	t.Setenv("DO_NOT_TRACK", "true")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"telemetry", "status"})
	if exitCode != ExitHealthy {
		t.Fatalf("status exit code = %d, want %d, stderr=%s", exitCode, ExitHealthy, stderr.String())
	}
	var statusPayload struct {
		Enabled    bool `json:"enabled"`
		DoNotTrack bool `json:"do_not_track"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &statusPayload); err != nil {
		t.Fatalf("status output is not JSON: %v; raw=%q", err, stdout.String())
	}
	if statusPayload.Enabled {
		t.Fatal("status enabled = true, want false under DO_NOT_TRACK")
	}
	if !statusPayload.DoNotTrack {
		t.Fatal("status do_not_track = false, want true")
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = runner.Run(context.Background(), []string{"telemetry", "log"})
	if exitCode != ExitDown {
		t.Fatalf("log exit code = %d, want %d", exitCode, ExitDown)
	}
}

func TestRunner_TelemetryCommand_UnknownSubcommandFails(t *testing.T) {
	var stderr bytes.Buffer
	runner := Runner{
		Stderr: &stderr,
	}

	exitCode := runner.Run(context.Background(), []string{"telemetry", "flush"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if got := stderr.String(); got == "" {
		t.Fatal("stderr is empty, want unknown telemetry subcommand message")
	}
}
