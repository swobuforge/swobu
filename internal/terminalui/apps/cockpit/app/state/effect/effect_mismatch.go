package effect

import (
	"context"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
)

var restartDaemon = startDaemonRestart

// MismatchRestartHintEffect reports the recommended recovery command.
type MismatchRestartHintEffect struct {
	Command string
}

func (eff MismatchRestartHintEffect) Run(ctx context.Context) any {
	_ = strings.TrimSpace(eff.Command) // swobu:io-string source=boundary
	message := restartDaemonMismatchMessage(ctx)
	return MismatchRecoveryNoted{
		Message: message,
		Action:  "run",
	}
}

// CopyExchangeDiagnosticsEffect copies mismatch diagnostics text.
type CopyExchangeDiagnosticsEffect struct {
	Text string
}

func (eff CopyExchangeDiagnosticsEffect) Run(context.Context) any {
	return MismatchRecoveryNoted{
		Message: copyValueNote(strings.TrimSpace(eff.Text)), // swobu:io-string source=boundary
		Action:  "copy",
	}
}

// ControlPlaneIncompatibleDetected marks daemon/TUI protocol mismatch.
type ControlPlaneIncompatibleDetected struct {
	ExpectedProtocol  int
	DaemonProtocol    int
	HasDaemonProtocol bool
	TUIVersion        string
	DaemonVersion     string
	Reason            string
}

// MismatchRecoveryNoted reports operator-facing recovery/copy outcome.
type MismatchRecoveryNoted struct {
	Message string
	Action  string
}

func restartDaemonMismatchMessage(ctx context.Context) string {
	err := restartDaemon(ctx, daemonlifecycle.RestartInput{})
	if err != nil {
		return "failed to restart daemon: " + strings.TrimSpace(err.Error()) // swobu:io-string source=boundary
	}
	return "daemon restart started"
}

func startDaemonRestart(ctx context.Context, in daemonlifecycle.RestartInput) error {
	if ctx == nil {
		ctx = context.Background()
	}
	restartCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return daemonlifecycle.Restart(restartCtx, in)
}
