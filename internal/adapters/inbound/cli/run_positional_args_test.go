package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/bootstrap"
)

// Leaf commands take only flags; a leftover positional is a user error and must
// not be a silent no-op.

func TestRunner_DaemonRejectsUnexpectedPositionalArg(t *testing.T) {
	t.Setenv("SWOBU_HOME", filepath.Join(t.TempDir(), "swobu-home"))
	t.Setenv("SWOBU_SKIP_TELEMETRY_NOTICE", "1")

	var stdout strings.Builder
	var stderr strings.Builder
	started := false
	runner := Runner{
		Stdout: &stdout,
		Stderr: &stderr,
		Start: func(context.Context, bootstrap.StartInput) (*bootstrap.Daemon, error) {
			started = true
			return nil, fmt.Errorf("daemon must not start on a stray positional")
		},
	}

	exitCode := runner.Run(context.Background(), []string{"daemon", "extra"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if started {
		t.Fatal("daemon start was called for a stray positional argument")
	}
	if got := stderr.String(); !strings.Contains(got, `unexpected argument "extra"`) {
		t.Fatalf("stderr missing rejection message; stderr=%q", got)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout must be empty (rejection happens before splash); stdout=%q", stdout.String())
	}
}

func TestRunner_StatusRejectsUnexpectedPositionalArg(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"status", "extra"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	// status surfaces usage on stdout; its stderr writer is unused for output.
	if got := stdout.String(); !strings.Contains(got, `unexpected argument "extra"`) {
		t.Fatalf("stdout missing rejection message; stdout=%q", got)
	}
}

func TestRunner_DaemonDownRejectsUnexpectedPositionalArg(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"daemon", "down", "extra"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if got := stderr.String(); !strings.Contains(got, `unexpected argument "extra"`) {
		t.Fatalf("stderr missing rejection message; stderr=%q", got)
	}
}

func TestRunner_DaemonDownOwnsShutdownHelp(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"daemon", "down", "--help"})
	if exitCode != ExitHealthy {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitHealthy)
	}
	if got := stderr.String(); !strings.Contains(got, "usage: swobu daemon down") {
		t.Fatalf("stderr missing nested shutdown usage; stderr=%q", got)
	}
}

func TestRunner_RootDownIsNotACommand(t *testing.T) {
	var stderr strings.Builder
	runner := Runner{Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"down"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if got := stderr.String(); !strings.Contains(got, `unknown subcommand "down"`) {
		t.Fatalf("stderr missing removed-command error; stderr=%q", got)
	}
}

func TestRunner_TelemetryStatusRejectsUnexpectedPositionalArg(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"telemetry", "status", "extra"})
	if exitCode != ExitDown {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitDown)
	}
	if got := stderr.String(); !strings.Contains(got, `unexpected argument "extra"`) {
		t.Fatalf("stderr missing rejection message; stderr=%q", got)
	}
}
