package effect

import (
	"context"
	"errors"
	"testing"

	"github.com/metrofun/swobu/internal/app/operator/daemonlifecycle"
)

func TestRestartDaemonCompatibilityMessage_Failure(t *testing.T) {
	orig := restartDaemon
	restartDaemon = func(context.Context, daemonlifecycle.RestartInput) error { return errors.New("boom") }
	t.Cleanup(func() { restartDaemon = orig })

	got := restartDaemonCompatibilityMessage(context.Background())
	if got != "failed to restart daemon: boom" {
		t.Fatalf("message=%q", got)
	}
}

func TestRestartDaemonCompatibilityMessage_Success(t *testing.T) {
	orig := restartDaemon
	restartDaemon = func(context.Context, daemonlifecycle.RestartInput) error { return nil }
	t.Cleanup(func() { restartDaemon = orig })

	got := restartDaemonCompatibilityMessage(context.Background())
	if got != "daemon restart started" {
		t.Fatalf("message=%q", got)
	}
}

func TestCompatibilityRestartHintEffect_ExecutesRestart(t *testing.T) {
	orig := restartDaemon
	restartDaemon = func(context.Context, daemonlifecycle.RestartInput) error { return nil }
	t.Cleanup(func() { restartDaemon = orig })

	actions := (CompatibilityRestartHintEffect{Command: "restart daemon"}).Execute(context.Background())
	if len(actions) != 1 {
		t.Fatalf("actions length=%d", len(actions))
	}
	noted, ok := actions[0].(CompatibilityRecoveryNoted)
	if !ok {
		t.Fatalf("action type=%T", actions[0])
	}
	if noted.Message != "daemon restart started" {
		t.Fatalf("message=%q", noted.Message)
	}
}
