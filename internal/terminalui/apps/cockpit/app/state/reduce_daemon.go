package state

import (
	"strings"

	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	"github.com/swobuforge/swobu/internal/terminalui/apps/shared/daemonstate"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func allowWhileControlPlaneIncompatible(action update.Action) bool {
	switch action.(type) {
	case stateeffect.ControlPlaneIncompatibleDetected,
		stateeffect.ReplaceDaemonStatus,
		stateeffect.DaemonStatusLoadFailed,
		stateeffect.DaemonRefreshTick,
		SetHelpTabOpenAction,
		OpenSupportLinkRequested,
		HelpDiagnosticsCopyRequested,
		SetInteractionMode,
		SetFocusedRowAffordance,
		CompatibilityRestartRequested,
		CompatibilityDiagnosticsCopyRequested,
		stateeffect.CompatibilityRecoveryNoted:
		return true
	default:
		return false
	}
}

func reduceDaemonState(model *Model, action update.Action) bool {
	switch value := action.(type) {
	case stateeffect.ControlPlaneIncompatibleDetected:
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
	case stateeffect.ReplaceDaemonStatus:
		model.ControlPlane = nil
		model.HeaderStatus = daemonstate.HeaderReady
		model.DaemonHint = ""
		model.FooterShowTabs = true
		switch strings.TrimSpace(value.State) {
		case "healthy":
			model.DaemonState = daemonstate.DaemonStateUp
		case "uninitialized":
			model.DaemonState = daemonstate.DaemonStateUninitialized
		default:
			model.DaemonState = daemonstate.DaemonStateUp
		}
		if value.EndpointCount == 0 && model.DaemonState == daemonstate.DaemonStateUninitialized {
			model.DaemonHint = "no endpoints"
		}
		return true
	case stateeffect.DaemonStatusLoadFailed:
		if model.ControlPlane != nil {
			model.HeaderStatus = "incompatible"
			model.DaemonState = "incompatible"
			model.DaemonHint = "daemon mismatch"
			model.ControlPlane.Note = strings.TrimSpace(value.Message)
			model.ControlPlane.NoteAction = ""
			return true
		}
		model.HeaderStatus = daemonstate.HeaderOfflineStale
		model.DaemonState = daemonstate.DaemonStateUnreachable
		model.DaemonHint = strings.TrimSpace(value.Message)
		return true
	default:
		return false
	}
}
