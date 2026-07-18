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
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))

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
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))

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

func TestRunner_DaemonAddrFlagOverridesEnvironment(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))
	t.Setenv("SWOBU_SKIP_TELEMETRY_NOTICE", "1")
	t.Setenv("SWOBU_ADDR", "127.0.0.1:8123")

	var stderr bytes.Buffer
	runner := Runner{
		Stderr: &stderr,
		Start: func(_ context.Context, input bootstrap.StartInput) (*bootstrap.Daemon, error) {
			if input.StartupConfig.Addr != "127.0.0.1:9000" {
				t.Fatalf("address = %q, want flag value", input.StartupConfig.Addr)
			}
			return nil, fmt.Errorf("stop after listen check")
		},
	}

	exitCode := runner.Run(context.Background(), []string{"daemon", "--addr", "127.0.0.1:9000"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
}

func TestRunner_DaemonUsesEnvironmentAddrWhenFlagOmitted(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))
	t.Setenv("SWOBU_SKIP_TELEMETRY_NOTICE", "1")
	t.Setenv("SWOBU_ADDR", "127.0.0.1:8123")

	runner := Runner{
		Start: func(_ context.Context, input bootstrap.StartInput) (*bootstrap.Daemon, error) {
			if input.StartupConfig.Addr != "127.0.0.1:8123" {
				t.Fatalf("address = %q, want environment value", input.StartupConfig.Addr)
			}
			return nil, fmt.Errorf("stop after listen check")
		},
	}
	if exitCode := runner.Run(context.Background(), []string{"daemon"}); exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
}

func TestRunner_DaemonRejectsNonLoopbackAddrBeforeStart(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))
	t.Setenv("SWOBU_SKIP_TELEMETRY_NOTICE", "1")
	started := false
	runner := Runner{
		Start: func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error) {
			started = true
			return nil, nil
		},
	}
	if exitCode := runner.Run(context.Background(), []string{"daemon", "--addr", "0.0.0.0:7926"}); exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if started {
		t.Fatal("daemon start called for non-loopback address")
	}
}
