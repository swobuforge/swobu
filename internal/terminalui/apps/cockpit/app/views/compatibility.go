package views

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildCompatibilityScreen renders the hard-stop cockpit body used when the
// daemon control plane does not match the local TUI protocol contract.
func BuildCompatibilityScreen(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	mismatch := model.ControlPlane
	if mismatch == nil {
		return retained.VStack(ctx)
	}
	compatibilityRows := []retained.ViewSpec[state.Model]{
		RowActionWithHooks(
			"status",
			"TUI and daemon are incompatible",
			"view",
			func() []update.Action { return nil },
			nil,
			focusAffordance("run/copy", false),
		),
		RowStatic("tui version", strings.TrimSpace(mismatch.TUIVersion)),
		RowStatic("daemon version", strings.TrimSpace(mismatch.DaemonVersion)),
		RowStatic("protocol", compatibilityProtocolLine(*mismatch)),
	}
	const restartDaemonLabel = "restart daemon"
	recoveryCommand := compatibilityRecoveryCommand(*mismatch)
	recoverRows := []retained.ViewSpec[state.Model]{
		RowActionWithHooks(restartDaemonLabel, "", "run", func() []update.Action {
			return []update.Action{state.CompatibilityRestartRequested{}}
		}, nil, focusAffordance("run/copy", false)),
	}
	if shouldRenderCompatibilityRecoveryDetail(restartDaemonLabel, recoveryCommand) {
		recoverRows = append(recoverRows, compatibilityDetailLine(recoveryCommand))
	}
	if strings.TrimSpace(mismatch.Note) != "" && strings.TrimSpace(mismatch.NoteAction) == "run" {
		recoverRows = append(recoverRows, compatibilityDetailLine("-> "+strings.TrimSpace(mismatch.Note)))
	}
	recoverRows = append(recoverRows,
		RowStatic("", ""),
		RowActionWithHooks("copy diagnostics", "", "copy", func() []update.Action {
			return []update.Action{state.CompatibilityDiagnosticsCopyRequested{}}
		}, nil, focusAffordance("run/copy", false)),
		compatibilityDetailLine("swobu "+strings.TrimSpace(mismatch.TUIVersion)),
		compatibilityDetailLine("daemon "+strings.TrimSpace(mismatch.DaemonVersion)),
		compatibilityDetailLine(compatibilityProtocolMismatchLine(*mismatch)),
	)
	if strings.TrimSpace(mismatch.Note) != "" && strings.TrimSpace(mismatch.NoteAction) == "copy" {
		recoverRows = append(recoverRows, compatibilityDetailLine("-> "+strings.TrimSpace(mismatch.Note)))
	}
	return retained.VStackGap(ctx, StackGap,
		Section[state.Model]("compatibility", compatibilityRows...),
		Section[state.Model]("recover", recoverRows...),
	)
}

func compatibilityRecoveryCommand(mismatch state.ControlPlaneMismatch) string {
	command := strings.TrimSpace(mismatch.RecoveryCommand)
	if command == "" {
		return "restart daemon"
	}
	return command
}

func compatibilityProtocolLine(mismatch state.ControlPlaneMismatch) string {
	if !mismatch.HasDaemonProtocol {
		return fmt.Sprintf("expected %d, got missing", mismatch.ExpectedProtocol)
	}
	return fmt.Sprintf("expected %d, got %d", mismatch.ExpectedProtocol, mismatch.DaemonProtocol)
}

func compatibilityProtocolMismatchLine(mismatch state.ControlPlaneMismatch) string {
	if !mismatch.HasDaemonProtocol {
		return fmt.Sprintf("protocol mismatch: expected %d, got missing", mismatch.ExpectedProtocol)
	}
	return fmt.Sprintf("protocol mismatch: expected %d, got %d", mismatch.ExpectedProtocol, mismatch.DaemonProtocol)
}

func compatibilityDetailLine(value string) retained.ViewSpec[state.Model] {
	return IndentLeft[state.Model](StaticTextLine[state.Model](strings.TrimSpace(value)), InsetSection+InsetDetail)
}

func shouldRenderCompatibilityRecoveryDetail(label, detail string) bool {
	normalizedLabel := strings.ToLower(strings.TrimSpace(label))
	normalizedDetail := strings.ToLower(strings.TrimSpace(detail))
	if normalizedDetail == "" {
		return false
	}
	return normalizedDetail != normalizedLabel
}
