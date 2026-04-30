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
		Enabled    bool `json:"enabled"`
		DoNotTrack bool `json:"do_not_track"`
	}
	if err := json.Unmarshal([]byte(out), &statusPayload); err != nil {
		t.Fatalf("status output is not JSON: %v; out=%q", err, out)
	}
	if !statusPayload.Enabled {
		t.Fatal("status enabled = false, want true")
	}
	if statusPayload.DoNotTrack {
		t.Fatal("status do_not_track = true, want false")
	}

	out, exitCode = runSwobuWithEnv(t, env, "telemetry", "off")
	if exitCode != 0 {
		t.Fatalf("off exit code = %d, want 0; out=%s", exitCode, out)
	}

	out, exitCode = runSwobuWithEnv(t, env, "telemetry", "on")
	if exitCode != 0 {
		t.Fatalf("on exit code = %d, want 0; out=%s", exitCode, out)
	}

	out, exitCode = runSwobuWithEnv(t, env, "telemetry", "log")
	if exitCode == 0 {
		t.Fatalf("log exit code = %d, want non-zero; out=%s", exitCode, out)
	}

	out, exitCode = runSwobuWithEnv(t, env, "telemetry", "flush")
	if exitCode == 0 {
		t.Fatalf("flush exit code = %d, want non-zero; out=%s", exitCode, out)
	}
	if !strings.Contains(out, "unknown telemetry subcommand") {
		t.Fatalf("output = %q, want unknown telemetry subcommand message", out)
	}
}

func TestCLI_TelemetryNamespaceContract_DoNotTrackOverride(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	env := map[string]string{
		"SWOBU_TELEMETRY_STATE_PATH": statePath,
		"DO_NOT_TRACK":               "true",
	}

	out, exitCode := runSwobuWithEnv(t, env, "telemetry", "status")
	if exitCode != 0 {
		t.Fatalf("status exit code = %d, want 0; out=%s", exitCode, out)
	}
	var statusPayload struct {
		Enabled    bool `json:"enabled"`
		DoNotTrack bool `json:"do_not_track"`
	}
	if err := json.Unmarshal([]byte(out), &statusPayload); err != nil {
		t.Fatalf("status output is not JSON: %v; out=%q", err, out)
	}
	if statusPayload.Enabled {
		t.Fatal("status enabled = true, want false with DO_NOT_TRACK")
	}
	if !statusPayload.DoNotTrack {
		t.Fatal("status do_not_track = false, want true")
	}

	out, exitCode = runSwobuWithEnv(t, env, "telemetry", "log")
	if exitCode == 0 {
		t.Fatalf("log exit code = %d, want non-zero; out=%s", exitCode, out)
	}
}

func TestCLI_TelemetryNamespaceContract_DoNotTrackYesOverride(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	env := map[string]string{
		"SWOBU_TELEMETRY_STATE_PATH": statePath,
		"DO_NOT_TRACK":               "yes",
	}

	out, exitCode := runSwobuWithEnv(t, env, "telemetry", "status")
	if exitCode != 0 {
		t.Fatalf("status exit code = %d, want 0; out=%s", exitCode, out)
	}
	var statusPayload struct {
		Enabled    bool `json:"enabled"`
		DoNotTrack bool `json:"do_not_track"`
	}
	if err := json.Unmarshal([]byte(out), &statusPayload); err != nil {
		t.Fatalf("status output is not JSON: %v; out=%q", err, out)
	}
	if statusPayload.Enabled {
		t.Fatal("status enabled = true, want false with DO_NOT_TRACK=yes")
	}
	if !statusPayload.DoNotTrack {
		t.Fatal("status do_not_track = false, want true with DO_NOT_TRACK=yes")
	}
}

func TestCLI_TelemetryDisclosureE2E_ShownBeforeDaemonLaunchAndPersisted(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	missingConfig := filepath.Join(t.TempDir(), "missing-config.yaml")
	env := map[string]string{
		"SWOBU_TELEMETRY_STATE_PATH": statePath,
	}

	out, exitCode := runSwobuWithEnv(t, env, "daemon", "--config", missingConfig)
	if exitCode == 0 {
		t.Fatalf("daemon exit code = %d, want non-zero for missing config; out=%s", exitCode, out)
	}
	if !strings.Contains(out, "Swobu sends anonymous aggregate reliability and usage summaries") {
		t.Fatalf("daemon output missing disclosure notice; out=%q", out)
	}
	if !strings.Contains(out, "export DO_NOT_TRACK=true") {
		t.Fatalf("daemon output missing DO_NOT_TRACK disclosure; out=%q", out)
	}

	statusOut, statusExit := runSwobuWithEnv(t, env, "telemetry", "status")
	if statusExit != 0 {
		t.Fatalf("telemetry status exit code = %d, want 0; out=%s", statusExit, statusOut)
	}
	var statusPayload struct {
		NoticeShown bool `json:"notice_shown"`
	}
	if err := json.Unmarshal([]byte(statusOut), &statusPayload); err != nil {
		t.Fatalf("status output is not JSON: %v; out=%q", err, statusOut)
	}
	if !statusPayload.NoticeShown {
		t.Fatal("notice_shown = false, want true after daemon launch attempt")
	}
}

func TestCLI_TelemetryDisclosureE2E_OneShotNoticeNotRepeated(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	missingConfig := filepath.Join(t.TempDir(), "missing-config.yaml")
	env := map[string]string{
		"SWOBU_TELEMETRY_STATE_PATH": statePath,
	}

	firstOut, firstExit := runSwobuWithEnv(t, env, "daemon", "--config", missingConfig)
	if firstExit == 0 {
		t.Fatalf("first daemon exit code = %d, want non-zero for missing config; out=%s", firstExit, firstOut)
	}
	secondOut, secondExit := runSwobuWithEnv(t, env, "daemon", "--config", missingConfig)
	if secondExit == 0 {
		t.Fatalf("second daemon exit code = %d, want non-zero for missing config; out=%s", secondExit, secondOut)
	}

	needle := "Swobu sends anonymous aggregate reliability and usage summaries"
	if !strings.Contains(firstOut, needle) {
		t.Fatalf("first output missing disclosure notice; out=%q", firstOut)
	}
	if strings.Contains(secondOut, needle) {
		t.Fatalf("second output repeated disclosure notice; out=%q", secondOut)
	}
}
