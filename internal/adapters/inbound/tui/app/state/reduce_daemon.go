package state

import (
	"strings"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
)

func allowWhileControlPlaneIncompatible(action update.Action) bool {
	switch action.(type) {
	case ControlPlaneIncompatibleDetected,
		ReplaceDaemonStatus,
		DaemonStatusLoadFailed,
		DaemonRefreshTick,
		SetHelpTabOpenAction,
		OpenSupportLinkRequested,
		HelpDiagnosticsCopyRequested,
		SetInteractionMode,
		SetFocusedRowAffordance,
		CompatibilityRestartRequested,
		CompatibilityDiagnosticsCopyRequested,
		CompatibilityRecoveryNoted:
		return true
	default:
		return false
	}
}

func reduceDaemonState(model *Model, action update.Action) bool {
	switch value := action.(type) {
	case ControlPlaneIncompatibleDetected:
		reason := strings.TrimSpace(value.Reason)
		if reason == "" {
			reason = "control-plane protocol mismatch"
		}
		model.ControlPlane = &ControlPlaneMismatch{
			ExpectedProtocol:  value.ExpectedProtocol,
			DaemonProtocol:    value.DaemonProtocol,
			HasDaemonProtocol: value.HasDaemonProtocol,
			TUIVersion:        strings.TrimSpace(value.TUIVersion),
			DaemonVersion:     strings.TrimSpace(value.DaemonVersion),
			Reason:            reason,
			RecoveryCommand:   "restart daemon",
			Note:              "",
			NoteAction:        "",
		}
		model.HeaderStatus = "incompatible"
		model.DaemonState = "incompatible"
		model.DaemonHint = "daemon mismatch"
		model.InteractionMode = InteractionModeNAV
		model.FooterVerb = "run/copy"
		model.FooterAllowSpace = false
		model.FooterShowTabs = false
		model.WorkspaceCopyNote = ""
		model.ClientCopyNote = ""
		model.ClientLaunchNote = ""
		model.ClientAccessNote = ""
		return true
	case ReplaceDaemonStatus:
		model.ControlPlane = nil
		model.HeaderStatus = "ready"
		model.DaemonHint = ""
		model.FooterShowTabs = true
		switch strings.TrimSpace(value.State) {
		case "healthy":
			model.DaemonState = "up"
		case "uninitialized":
			model.DaemonState = "uninitialized"
		default:
			model.DaemonState = "up"
		}
		if value.EndpointCount == 0 && model.DaemonState == "uninitialized" {
			model.DaemonHint = "no endpoints"
		}
		return true
	case DaemonStatusLoadFailed:
		if model.ControlPlane != nil {
			model.HeaderStatus = "incompatible"
			model.DaemonState = "incompatible"
			model.DaemonHint = "daemon mismatch"
			model.ControlPlane.Note = strings.TrimSpace(value.Message)
			model.ControlPlane.NoteAction = ""
			return true
		}
		model.HeaderStatus = "offline (stale)"
		model.DaemonState = "unreachable"
		model.DaemonHint = strings.TrimSpace(value.Message)
		return true
	default:
		return false
	}
}
