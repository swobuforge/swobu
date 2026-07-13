package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/components/compound"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

const cockpitActionSignalKind = "cockpit.action"
const cockpitRowFocusSignalKind = "cockpit.row.focus"
const cockpitStaticRowSignalKind = "cockpit.row.static"

// CoreNodeAsRetained lowers one semantic core node into a retained view spec.
func CoreNodeAsRetained(node core.Node[state.Action]) retained.ViewSpec[state.Model] {
	return retained.View[state.Model](func(ctx *retained.Context[state.Model]) retained.RenderNode {
		renderNode, err := corelower.Lower(node, corelower.EnvConfig{}, identityCaster)
		if err != nil {
			return nil
		}
		return renderNode
	})
}

func identityCaster(action state.Action) update.Action {
	return action
}

// SettingActionRowNode returns the semantic core node for one action row.
// Use this in pure-core composition; use SettingActionRow in retained views.
func SettingActionRowNode(
	key core.Key,
	label, value, verb string,
	action state.Action,
	disabled bool,
) core.Node[state.Action] {
	normalizedVerb := strings.TrimSpace(verb) // swobu:io-string source=boundary
	if normalizedVerb == "" {
		normalizedVerb = "act"
	}
	var focusSignal core.SignalEvent[state.Action]
	if !disabled {
		focusSignal = core.SignalEvent[state.Action]{
			Kind:  cockpitRowFocusSignalKind,
			Event: state.SetFocusedRowAffordance{Verb: normalizedVerb, AllowSpace: false},
		}
	}
	var activationSignal core.SignalEvent[state.Action]
	if action != nil {
		activationSignal = core.SignalEvent[state.Action]{
			Kind:  cockpitActionSignalKind,
			Event: action,
		}
	}
	return settingRowNode(key, label, value, normalizedVerb+" ↵", activationSignal, focusSignal, disabled)
}

// SettingActionRow returns one app-owned semantic action row wrapped for
// retained composition. Prefer SettingActionRowNode for core-only paths.
func SettingActionRow(
	key core.Key,
	label, value, verb string,
	action state.Action,
	disabled bool,
) retained.ViewSpec[state.Model] {
	node := SettingActionRowNode(key, label, value, verb, action, disabled)
	return CoreNodeAsRetained(node)
}

// SettingStaticRow returns one non-interactive semantic row for plain values.
func SettingStaticRow(label, value string) retained.ViewSpec[state.Model] {
	node := settingRowNode(core.K(""), label, value, "", core.SignalEvent[state.Action]{Kind: cockpitStaticRowSignalKind}, core.SignalEvent[state.Action]{}, true)
	return CoreNodeAsRetained(node)
}

func settingRowNode(
	key core.Key,
	label, value, actionLabel string,
	signal core.SignalEvent[state.Action],
	focusSignal core.SignalEvent[state.Action],
	disabled bool,
) core.Node[state.Action] {
	return compound.SettingRow(compound.SettingRowProps[state.Action]{
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
	})
}
