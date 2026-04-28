package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_TelemetryNamespaceContract_UltraLean(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	env := map[string]string{
		"SWOBU_TELEMETRY_STATE_PATH": statePath,
	}

	out, exitCode := runSwobuWithEnv(t, env, "telemetry", "status")
	if exitCode != 0 {
		t.Fatalf("status exit code = %d, want 0; out=%s", exitCode, out)
	}
	var statusPayload struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(out), &statusPayload); err != nil {
		t.Fatalf("status output is not JSON: %v; out=%q", err, out)
	}
	if !statusPayload.Enabled {
		t.Fatal("status enabled = false, want true")
	}

	out, exitCode = runSwobuWithEnv(t, env, "telemetry", "off")
	if exitCode != 0 {
		t.Fatalf("off exit code = %d, want 0; out=%s", exitCode, out)
	}

	out, exitCode = runSwobuWithEnv(t, env, "telemetry", "on")
	if exitCode != 0 {
		t.Fatalf("on exit code = %d, want 0; out=%s", exitCode, out)
	}

	out, exitCode = runSwobuWithEnv(t, env, "telemetry", "inspect")
	if exitCode != 0 {
		t.Fatalf("inspect exit code = %d, want 0; out=%s", exitCode, out)
	}
	var inspectPayload struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(out), &inspectPayload); err != nil {
		t.Fatalf("inspect output is not JSON: %v; out=%q", err, out)
	}
	if inspectPayload.Kind != "install_summary" {
		t.Fatalf("kind = %q, want install_summary", inspectPayload.Kind)
	}

	out, exitCode = runSwobuWithEnv(t, env, "telemetry", "show-payload")
	if exitCode != 0 {
		t.Fatalf("show-payload exit code = %d, want 0; out=%s", exitCode, out)
	}
	if err := json.Unmarshal([]byte(out), &inspectPayload); err != nil {
		t.Fatalf("show-payload output is not JSON: %v; out=%q", err, out)
	}
	if inspectPayload.Kind != "install_summary" {
		t.Fatalf("kind = %q, want install_summary", inspectPayload.Kind)
	}

	out, exitCode = runSwobuWithEnv(t, env, "telemetry", "reset")
	if exitCode != 0 {
		t.Fatalf("reset exit code = %d, want 0; out=%s", exitCode, out)
	}
	var resetPayload struct {
		Enabled            bool   `json:"enabled"`
		AnonymousInstallID string `json:"anonymous_install_id"`
		NoticeShown        bool   `json:"notice_shown"`
	}
	if err := json.Unmarshal([]byte(out), &resetPayload); err != nil {
		t.Fatalf("reset output is not JSON: %v; out=%q", err, out)
	}
	if !resetPayload.Enabled {
		t.Fatal("reset enabled = false, want true")
	}
	if resetPayload.AnonymousInstallID == "" {
		t.Fatal("reset anonymous_install_id is empty")
	}
	if resetPayload.NoticeShown {
		t.Fatal("reset notice_shown = true, want false")
	}

	out, exitCode = runSwobuWithEnv(t, env, "telemetry", "flush")
	if exitCode == 0 {
		t.Fatalf("flush exit code = %d, want non-zero; out=%s", exitCode, out)
	}
	if !strings.Contains(out, "unknown telemetry subcommand") {
		t.Fatalf("output = %q, want unknown telemetry subcommand message", out)
	}
}
