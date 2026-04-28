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
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &statusPayload); err != nil {
		t.Fatalf("status output is not JSON: %v; raw=%q", err, stdout.String())
	}
	if !statusPayload.Enabled {
		t.Fatal("status enabled = false, want true")
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
	exitCode = runner.Run(context.Background(), []string{"telemetry", "show-payload"})
	if exitCode != ExitHealthy {
		t.Fatalf("show-payload exit code = %d, want %d, stderr=%s", exitCode, ExitHealthy, stderr.String())
	}
	var inspectPayload struct {
		Kind             string `json:"kind"`
		TelemetryEnabled bool   `json:"telemetry_enabled"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &inspectPayload); err != nil {
		t.Fatalf("inspect output is not JSON: %v; raw=%q", err, stdout.String())
	}
	if inspectPayload.Kind != "install_summary" {
		t.Fatalf("inspect kind = %q, want install_summary", inspectPayload.Kind)
	}
	if inspectPayload.TelemetryEnabled {
		t.Fatal("inspect telemetry_enabled = true, want false")
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = runner.Run(context.Background(), []string{"telemetry", "reset"})
	if exitCode != ExitHealthy {
		t.Fatalf("reset exit code = %d, want %d, stderr=%s", exitCode, ExitHealthy, stderr.String())
	}
	var resetPayload struct {
		Enabled            bool   `json:"enabled"`
		AnonymousInstallID string `json:"anonymous_install_id"`
		NoticeShown        bool   `json:"notice_shown"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resetPayload); err != nil {
		t.Fatalf("reset output is not JSON: %v; raw=%q", err, stdout.String())
	}
	if resetPayload.Enabled {
		t.Fatal("reset enabled = true, want false")
	}
	if resetPayload.AnonymousInstallID == "" {
		t.Fatal("reset anonymous_install_id is empty")
	}
	if resetPayload.NoticeShown {
		t.Fatal("reset notice_shown = true, want false")
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
