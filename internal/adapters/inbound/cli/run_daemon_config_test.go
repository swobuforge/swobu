package cli

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/bootstrap"
)

func TestRunner_DaemonUsesEnvConfigPathWhenFlagOmitted(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	t.Setenv("SWOBU_TELEMETRY_STATE_PATH", statePath)

	configPath := filepath.Join(t.TempDir(), "swobu.yaml")
	t.Setenv("SWOBU_CONFIG_PATH", configPath)

	var stderr bytes.Buffer
	runner := Runner{
		Stderr: &stderr,
		Start: func(_ context.Context, input bootstrap.StartInput) (*bootstrap.Daemon, error) {
			if input.ConfigPath != configPath {
				t.Fatalf("config path = %q, want %q", input.ConfigPath, configPath)
			}
			return nil, fmt.Errorf("stop after config path check")
		},
	}

	exitCode := runner.Run(context.Background(), []string{"daemon"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
}

func TestRunner_DaemonConfigFlagOverridesEnvConfigPath(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	t.Setenv("SWOBU_TELEMETRY_STATE_PATH", statePath)

	envConfigPath := filepath.Join(t.TempDir(), "env-swobu.yaml")
	t.Setenv("SWOBU_CONFIG_PATH", envConfigPath)
	flagConfigPath := filepath.Join(t.TempDir(), "flag-swobu.yaml")

	var stderr bytes.Buffer
	runner := Runner{
		Stderr: &stderr,
		Start: func(_ context.Context, input bootstrap.StartInput) (*bootstrap.Daemon, error) {
			if input.ConfigPath != flagConfigPath {
				t.Fatalf("config path = %q, want %q", input.ConfigPath, flagConfigPath)
			}
			return nil, fmt.Errorf("stop after config path check")
		},
	}

	exitCode := runner.Run(context.Background(), []string{"daemon", "--config", flagConfigPath})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
}
