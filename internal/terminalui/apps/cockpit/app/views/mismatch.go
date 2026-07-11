package views

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildMismatchScreen renders the hard-stop cockpit body used when the
// daemon control plane does not match the local TUI protocol contract.
func BuildMismatchScreen(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	mismatch := model.ControlPlane
	if mismatch == nil {
		return retained.VStack(ctx)
	}
	mismatchRows := []retained.ViewSpec[state.Model]{
		RowActionWithHooks(
			"status",
			"TUI and daemon are incompatible",
			"view",
			func() []update.Action { return nil },
			nil,
			focusAffordance("run/copy", false),
		),
		RowStatic("tui version", strings.TrimSpace(mismatch.TUIVersion)),       // swobu:io-string source=boundary
		RowStatic("daemon version", strings.TrimSpace(mismatch.DaemonVersion)), // swobu:io-string source=boundary
		RowStatic("protocol", mismatchProtocolLine(*mismatch)),
	}
	const restartDaemonLabel = "restart daemon"
	recoveryCommand := mismatchRecoveryCommand(*mismatch)
	recoverRows := []retained.ViewSpec[state.Model]{
		RowActionWithHooks(restartDaemonLabel, "", "run", func() []update.Action {
			return []update.Action{state.MismatchRestartRequested{}}
		}, nil, focusAffordance("run/copy", false)),
	}
	if shouldRenderMismatchRecoveryDetail(restartDaemonLabel, recoveryCommand) {
		recoverRows = append(recoverRows, mismatchDetailLine(recoveryCommand))
	}
	if strings.TrimSpace(mismatch.Note) != "" && strings.TrimSpace(mismatch.NoteAction) == "run" { // swobu:io-string source=boundary
		recoverRows = append(recoverRows, mismatchDetailLine("-> "+strings.TrimSpace(mismatch.Note))) // swobu:io-string source=boundary
	}
	recoverRows = append(recoverRows,
		RowStatic("", ""),
		RowActionWithHooks("copy diagnostics", "", "copy", func() []update.Action {
			return []update.Action{state.ExchangeDiagnosticsCopyRequested{}}
		}, nil, focusAffordance("run/copy", false)),
		mismatchDetailLine("swobu "+strings.TrimSpace(mismatch.TUIVersion)),     // swobu:io-string source=boundary
		mismatchDetailLine("daemon "+strings.TrimSpace(mismatch.DaemonVersion)), // swobu:io-string source=boundary
		mismatchDetailLine(mismatchProtocolMismatchLine(*mismatch)),
	)
	if strings.TrimSpace(mismatch.Note) != "" && strings.TrimSpace(mismatch.NoteAction) == "copy" { // swobu:io-string source=boundary
		recoverRows = append(recoverRows, mismatchDetailLine("-> "+strings.TrimSpace(mismatch.Note))) // swobu:io-string source=boundary
	}
	return retained.VStackGap(ctx, StackGap,
		Section[state.Model]("mismatch", mismatchRows...),
		Section[state.Model]("recover", recoverRows...),
	)
}

func mismatchRecoveryCommand(mismatch state.ControlPlaneMismatch) string {
	command := strings.TrimSpace(mismatch.RecoveryCommand) // swobu:io-string source=boundary
	if command == "" {
		return "restart daemon"
	}
	return command
}

func mismatchProtocolLine(mismatch state.ControlPlaneMismatch) string {
	if !mismatch.HasDaemonProtocol {
		return fmt.Sprintf("expected %d, got missing", mismatch.ExpectedProtocol)
	}
	return fmt.Sprintf("expected %d, got %d", mismatch.ExpectedProtocol, mismatch.DaemonProtocol)
}

func mismatchProtocolMismatchLine(mismatch state.ControlPlaneMismatch) string {
	if !mismatch.HasDaemonProtocol {
		return fmt.Sprintf("protocol mismatch: expected %d, got missing", mismatch.ExpectedProtocol)
	}
	return fmt.Sprintf("protocol mismatch: expected %d, got %d", mismatch.ExpectedProtocol, mismatch.DaemonProtocol)
}

func mismatchDetailLine(value string) retained.ViewSpec[state.Model] {
	return IndentLeft[state.Model](StaticTextLine[state.Model](strings.TrimSpace(value)), InsetSection+InsetDetail) // swobu:io-string source=boundary
}

func shouldRenderMismatchRecoveryDetail(label, detail string) bool {
	normalizedLabel := strings.ToLower(strings.TrimSpace(label))   // swobu:io-string source=boundary
	normalizedDetail := strings.ToLower(strings.TrimSpace(detail)) // swobu:io-string source=boundary
	if normalizedDetail == "" {
		return false
	}
	return normalizedDetail != normalizedLabel
}
