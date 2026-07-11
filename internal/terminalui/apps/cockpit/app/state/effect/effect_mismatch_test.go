package effect

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
)

func TestRestartDaemonMismatchMessage_Failure(t *testing.T) {
	orig := restartDaemon
	restartDaemon = func(context.Context, daemonlifecycle.RestartInput) error { return errors.New("boom") }
	t.Cleanup(func() { restartDaemon = orig })

	got := restartDaemonMismatchMessage(context.Background())
	if got != "failed to restart daemon: boom" {
		t.Fatalf("message=%q", got)
	}
}

func TestRestartDaemonMismatchMessage_Success(t *testing.T) {
	orig := restartDaemon
	restartDaemon = func(context.Context, daemonlifecycle.RestartInput) error { return nil }
	t.Cleanup(func() { restartDaemon = orig })

	got := restartDaemonMismatchMessage(context.Background())
	if got != "daemon restart started" {
		t.Fatalf("message=%q", got)
	}
}

func TestMismatchRestartHintEffect_ExecutesRestart(t *testing.T) {
	orig := restartDaemon
	restartDaemon = func(context.Context, daemonlifecycle.RestartInput) error { return nil }
	t.Cleanup(func() { restartDaemon = orig })

	actions := (MismatchRestartHintEffect{Command: "restart daemon"}).Execute(context.Background())
	if len(actions) != 1 {
		t.Fatalf("actions length=%d", len(actions))
	}
	noted, ok := actions[0].(MismatchRecoveryNoted)
	if !ok {
		t.Fatalf("action type=%T", actions[0])
	}
	if noted.Message != "daemon restart started" {
		t.Fatalf("message=%q", noted.Message)
	}
}
