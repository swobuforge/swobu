package effect

import (
	"context"
	"strings"
	"time"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
	"github.com/metrofun/swobu/internal/app/operator/daemonlifecycle"
)

var restartDaemon = startDaemonRestart

// CompatibilityRestartHintEffect reports the recommended recovery command.
type CompatibilityRestartHintEffect struct {
	Command string
}

func (eff CompatibilityRestartHintEffect) Execute(ctx context.Context) []update.Action {
	_ = strings.TrimSpace(eff.Command)
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
		Message: copyValueNote(strings.TrimSpace(eff.Text)),
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
		return "failed to restart daemon: " + strings.TrimSpace(err.Error())
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
