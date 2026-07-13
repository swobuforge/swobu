package views

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/components/compound"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildMismatchScreenNode returns the mismatch screen as a pure semantic
// core.Node.  This is the canonical core-algebra path for the mismatch screen.
func BuildMismatchScreenNode(model state.Model) core.Node[state.Action] {
	mismatch := model.ControlPlane
	if mismatch == nil {
		return core.Box[state.Action]()
	}

	mismatchRows := []core.Node[state.Action]{
		settingRowNode(
			core.K("mismatch/status"),
			"status",
			"TUI and daemon are incompatible",
			"view ↵",
			core.SignalEvent[state.Action]{Kind: cockpitStaticRowSignalKind},
			core.SignalEvent[state.Action]{
				Kind:  cockpitRowFocusSignalKind,
				Event: state.SetFocusedRowAffordance{Verb: "run/copy", AllowSpace: false},
			},
			false,
		),
		settingStaticRowNode("tui version", strings.TrimSpace(mismatch.TUIVersion)),
		settingStaticRowNode("daemon version", strings.TrimSpace(mismatch.DaemonVersion)),
		settingStaticRowNode("protocol", mismatchProtocolLine(*mismatch)),
	}

	const restartDaemonLabel = "restart daemon"
	recoveryCommand := mismatchRecoveryCommand(*mismatch)
	recoverRows := []core.Node[state.Action]{
		SettingActionRowNode(
			core.K("recover/restart-daemon"),
			restartDaemonLabel,
			"",
			"run",
			state.MismatchRestartRequested{},
			false,
		),
	}
	if shouldRenderMismatchRecoveryDetail(restartDaemonLabel, recoveryCommand) {
		recoverRows = append(recoverRows, mismatchDetailNode(recoveryCommand))
	}
	if strings.TrimSpace(mismatch.Note) != "" && strings.TrimSpace(mismatch.NoteAction) == "run" {
		recoverRows = append(recoverRows, mismatchDetailNode("-> "+strings.TrimSpace(mismatch.Note)))
	}
	recoverRows = append(recoverRows,
		settingStaticRowNode("", ""),
		SettingActionRowNode(
			core.K("recover/copy-diagnostics"),
			"copy diagnostics",
			"",
			"copy",
			state.ExchangeDiagnosticsCopyRequested{},
			false,
		),
		mismatchDetailNode("swobu "+strings.TrimSpace(mismatch.TUIVersion)),
		mismatchDetailNode("daemon "+strings.TrimSpace(mismatch.DaemonVersion)),
		mismatchDetailNode(mismatchProtocolMismatchLine(*mismatch)),
	)
	if strings.TrimSpace(mismatch.Note) != "" && strings.TrimSpace(mismatch.NoteAction) == "copy" {
		recoverRows = append(recoverRows, mismatchDetailNode("-> "+strings.TrimSpace(mismatch.Note)))
	}

	return core.Stack[state.Action](core.AxisVertical,
		compound.SectionNode[state.Action]("mismatch", mismatchRows...),
		compound.SectionNode[state.Action]("recover", recoverRows...),
	).Layout(core.Layout{
		Size: core.Size{Width: core.Fill(1), Height: core.Fit()},
		Flow: core.Flow{Mode: core.FlowStack, Axis: core.AxisVertical, Gap: StackGap},
	})
}

// BuildMismatchScreen is the retained bridge wrapper.
// It lowers the canonical core node into a retained ViewSpec for composition
// during the migration period. New code should prefer BuildMismatchScreenNode.
func BuildMismatchScreen(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	return CoreNodeAsRetained(BuildMismatchScreenNode(ctx.Model()))
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

func mismatchDetailNode(value string) core.Node[state.Action] {
	return core.Text[state.Action](strings.Repeat(" ", InsetSection+InsetDetail) + strings.TrimSpace(value)).
		Style(core.Style{Token: core.TokenTextDefault, State: core.StateDefault})
}

func shouldRenderMismatchRecoveryDetail(label, detail string) bool {
	normalizedLabel := strings.ToLower(strings.TrimSpace(label))   // swobu:io-string source=boundary
	normalizedDetail := strings.ToLower(strings.TrimSpace(detail)) // swobu:io-string source=boundary
	if normalizedDetail == "" {
		return false
	}
	return normalizedDetail != normalizedLabel
}
