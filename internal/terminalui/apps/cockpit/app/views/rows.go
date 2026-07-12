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
	verb = strings.TrimSpace(verb)
	if verb == "" {
		verb = "act"
	}
	focusSignal := core.Signal{}
	if !disabled {
		focusSignal = core.Signal{
			Kind: cockpitRowFocusSignalKind,
			Data: state.SetFocusedRowAffordance{Verb: verb, AllowSpace: false},
		}
	}
	activationSignal := core.Signal{}
	if action != nil {
		activationSignal = core.Signal{
			Kind: cockpitActionSignalKind,
			Data: action,
		}
	}
	return settingRow(key, label, value, verb+" ↵", activationSignal, focusSignal, disabled)
}

// SettingStaticRow returns one non-interactive semantic row for plain values.
func SettingStaticRow(label, value string) retained.ViewSpec[state.Model] {
	return settingRow(core.K(""), label, value, "", core.Signal{Kind: cockpitStaticRowSignalKind}, core.Signal{}, true)
}

func settingRow(
	key core.Key,
	label, value, actionLabel string,
	signal core.Signal,
	focusSignal core.Signal,
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
		Help: func() []core.HelpBinding {
			verb := strings.TrimSpace(strings.TrimSuffix(actionLabel, " ↵"))
			if verb == "" {
				return nil
			}
			return []core.HelpBinding{{Key: "↵", Label: verb}}
		}(),
	}))
}
