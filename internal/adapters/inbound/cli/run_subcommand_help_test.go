package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunner_DaemonHelp_PrintsConfigResolution(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"daemon", "--help"})
	if exitCode != ExitHealthy {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitHealthy)
	}
	if !strings.Contains(stderr.String(), "root daemon config path (env: SWOBU_CONFIG_PATH) (default:") {
		t.Fatalf("daemon help missing env/default metadata; stderr=%q", stderr.String())
	}
}

func TestRunner_TelemetryStatusHelp_DoesNotExposeStatePathKnobs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"telemetry", "status", "--help"})
	if exitCode != ExitHealthy {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitHealthy)
	}
	if strings.Contains(stderr.String(), "state-path") {
		t.Fatalf("telemetry status help exposes deprecated state path override; stderr=%q", stderr.String())
	}
}
