package effect

import (
	"context"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/daemonlifecycle"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

var restartDaemon = startDaemonRestart

// CompatibilityRestartHintEffect reports the recommended recovery command.
type CompatibilityRestartHintEffect struct {
	Command string
}

func (eff CompatibilityRestartHintEffect) Execute(ctx context.Context) []update.Action {
	_ = strings.TrimSpace(eff.Command) // swobu:io-string source=boundary
	message := restartDaemonCompatibilityMessage(ctx)
	return []update.Action{CompatibilityRecoveryNoted{
		Message: message,
		Action:  "run",
	}}
}

// CopyCompatibilityDiagnosticsEffect copies mismatch diagnostics text.
type CopyCompatibilityDiagnosticsEffect struct {
	Text string
}

func (eff CopyCompatibilityDiagnosticsEffect) Execute(context.Context) []update.Action {
	return []update.Action{CompatibilityRecoveryNoted{
		Message: copyValueNote(strings.TrimSpace(eff.Text)), // swobu:io-string source=boundary
		Action:  "copy",
	}}
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

// CompatibilityRecoveryNoted reports operator-facing recovery/copy outcome.
type CompatibilityRecoveryNoted struct {
	Message string
	Action  string
}

func restartDaemonCompatibilityMessage(ctx context.Context) string {
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
