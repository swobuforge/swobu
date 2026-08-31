package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/producttelemetry"
)

func TestRunner_TelemetryCommand_UltraLeanFlow(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir()+"/swobu-home")

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
	if status, _ := producttelemetry.Status(); status.Enabled {
		t.Fatal("preference not persisted as disabled after off")
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

func TestRunner_TelemetryInspectPrintsDaemonReport(t *testing.T) {
	report := `{"schema":1,"install_id":"0123456789abcdef0123456789abcdef","runtime":{"version":"0.1.0","os":"linux","arch":"amd64"},"installation_age_bucket":"1_7d","traffic":[],"overflow_count":1}`
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/_swobu/telemetry-report" || request.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		_, _ = response.Write([]byte(report))
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr, HTTPClient: server.Client()}
	exitCode := runner.Run(context.Background(), []string{"telemetry", "inspect", "--addr", strings.TrimPrefix(server.URL, "http://")})
	if exitCode != ExitHealthy {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["schema"] != float64(1) {
		t.Fatalf("schema=%v", got["schema"])
	}
}

func TestRunner_TelemetryCommand_DoNotTrackOverride(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir()+"/swobu-home")
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
	// enabled is the persisted preference, independent of DO_NOT_TRACK: no `off`
	// was issued, so it stays true; do_not_track reports the override. The caller
	// composes the two (effective upload is disabled by do_not_track here).
	if !statusPayload.Enabled {
		t.Fatal("status enabled = false, want true (persisted preference is independent of DO_NOT_TRACK)")
	}
	if !statusPayload.DoNotTrack {
		t.Fatal("status do_not_track = false, want true")
	}
}

func TestRunner_TelemetryCommand_UnknownSubcommandFails(t *testing.T) {
	var stderr bytes.Buffer
	runner := Runner{Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"telemetry", "flush"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if got := stderr.String(); !strings.Contains(got, `unknown telemetry subcommand "flush"`) {
		t.Fatalf("stderr missing unknown telemetry subcommand message; stderr=%q", got)
	}
}

// `telemetry off` writes the preference document under SWOBU_HOME.
func TestRunner_TelemetryCommand_WritesPreferenceUnderSwobuHome(t *testing.T) {
	root := filepath.Join(t.TempDir(), "swobu-home")
	t.Setenv("SWOBU_HOME", root)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"telemetry", "off"})
	if exitCode != ExitHealthy {
		t.Fatalf("off exit code = %d, want %d, stderr=%s", exitCode, ExitHealthy, stderr.String())
	}
	prefPath := filepath.Join(root, "state", "telemetry", "preference.json")
	status, err := producttelemetry.Status()
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status.Enabled {
		t.Fatalf("status.Enabled = true, want false (preference written under %s)", prefPath)
	}
}

func TestRunner_TelemetryCommand_StatePathFlagRejected(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir()+"/swobu-home")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"telemetry", "off", "--state-path", "/tmp/state.json"})
	if exitCode != ExitDown {
		t.Fatalf("off exit code = %d, want %d", exitCode, ExitDown)
	}
	if got := stderr.String(); !strings.Contains(got, "flag provided but not defined") {
		t.Fatalf("stderr missing unknown flag error; stderr=%q", got)
	}
}

func TestRunner_TelemetryCommand_MissingSubcommandShowsError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"telemetry"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if got := stderr.String(); !strings.Contains(got, "telemetry subcommand required: status|on|off|inspect") {
		t.Fatalf("stderr missing telemetry missing-subcommand error; stderr=%q", got)
	}
}
