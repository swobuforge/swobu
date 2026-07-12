package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/components/compound"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

const cockpitActionSignalKind = "cockpit.action"
const cockpitRowFocusSignalKind = "cockpit.row.focus"
const cockpitStaticRowSignalKind = "cockpit.row.static"

// SettingActionRow returns one app-owned semantic action row.
func SettingActionRow(
	key core.Key,
	label, value, verb string,
	action update.Action,
	disabled bool,
) retained.ViewSpec[state.Model] {
	normalizedVerb := strings.TrimSpace(verb) // swobu:io-string source=boundary
	if normalizedVerb == "" {
		normalizedVerb = "act"
	}
	focusSignal := core.SignalEvent{}
	if !disabled {
		focusSignal = core.SignalEvent{
			Kind: cockpitRowFocusSignalKind,
			Data: state.SetFocusedRowAffordance{Verb: normalizedVerb, AllowSpace: false},
		}
	}
	activationSignal := core.SignalEvent{}
	if action != nil {
		activationSignal = core.SignalEvent{
			Kind: cockpitActionSignalKind,
			Data: action,
		}
	}
	return settingRow(key, label, value, normalizedVerb+" ↵", activationSignal, focusSignal, disabled)
}

// SettingStaticRow returns one non-interactive semantic row for plain values.
func SettingStaticRow(label, value string) retained.ViewSpec[state.Model] {
	return settingRow(core.K(""), label, value, "", core.SignalEvent{Kind: cockpitStaticRowSignalKind}, core.SignalEvent{}, true)
}

func settingRow(
	key core.Key,
	label, value, actionLabel string,
	signal core.SignalEvent,
	focusSignal core.SignalEvent,
	disabled bool,
) retained.ViewSpec[state.Model] {
	return CoreNodeAsRetained[state.Model](compound.SettingRow(compound.SettingRowProps{
		Key:         key,
		Label:       label,
		Value:       value,
		ActionLabel: actionLabel,
		Signal:      signal,
		FocusSignal: focusSignal,
		Disabled:    disabled,
		Help: func() []core.HelpBindingSpec {
			actionVerb := strings.TrimSpace(strings.TrimSuffix(actionLabel, " ↵")) // swobu:io-string source=boundary
			if actionVerb == "" {
				return nil
			}
			return []core.HelpBindingSpec{{Key: "↵", Label: actionVerb}}
		}(),
	}))
}
