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

	action := (MismatchRestartHintEffect{Command: "restart daemon"}).Run(context.Background())
	noted, ok := action.(MismatchRecoveryNoted)
	if !ok {
		t.Fatalf("action type=%T", action)
	}
	if noted.Message != "daemon restart started" {
		t.Fatalf("message=%q", noted.Message)
	}
}
