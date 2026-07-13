// Package primitive provides thin props-adapters over core.Node constructors.
//
// primitive.Action is a convenience wrapper around core.Action that maps
// a typed props struct onto the core constructor, handling focus-mode
// switching when disabled and deriving default help text. It does not
// change the semantics of core.Action; callers may always use core.Action
// directly for full control.
//
// Rule of thumb: core.Action owns the semantic contract and signal wiring;
// primitive.Action owns the props-to-node mapping for cockpit-style rows.
package primitive

import "github.com/swobuforge/swobu/internal/terminalui/core"

// ActionProps describes one focusable semantic action row and its emitted
// activation and focus signals.
type ActionProps[E any] struct {
	Key    core.Key
	Label  string
	Signal core.SignalEvent[E]
	// FocusSignal is emitted when the action gains focus.
	FocusSignal core.SignalEvent[E]
	Disabled    bool
	Help        []core.HelpBindingSpec
}

// Action returns one semantic action node with enter binding and optional
// activation/focus signals.
func Action[E any](p ActionProps[E]) core.Node[E] {
	help := p.Help
	if len(help) == 0 {
		help = []core.HelpBindingSpec{{Key: "enter", Label: "activate"}}
	}

	state := core.StateDefault
	token := core.TokenTextDefault
	focusMode := core.Focusable
	if p.Disabled {
		state = core.StateDisabled
		token = core.TokenTextMuted
		focusMode = core.FocusNone
	}

	signals := make([]core.SignalEvent[E], 0, 2)
	contractSignals := make([]core.SignalSpec[E], 0, 2)
	if p.Signal.Kind != "" {
		signals = append(signals, p.Signal)
		contractSignals = append(contractSignals, core.SignalSpec[E]{Kind: p.Signal.Kind})
	}

	return core.Action[E](p.Label, p.Signal).
		Key(p.Key).
		Style(core.Style{Token: token, State: state}).
		Interaction(core.InteractionSpec[E]{
			Focus:   core.FocusSpec{Mode: focusMode},
			Keymap:  []core.KeyBindingSpec{{Pattern: core.KeyEnter(), Intent: core.IntentActivate}},
			Help:    help,
			Signals: signals,
			FocusSignals: func() []core.SignalEvent[E] {
				if p.FocusSignal.Kind == "" {
					return nil
				}
				return []core.SignalEvent[E]{p.FocusSignal}
			}(),
		}).
		Contract(core.Contract[E]{
			Name:    "Action",
			Purpose: "Focusable semantic action.",
			Signals: contractSignals,
			Help:    help,
			Focus: core.FocusPolicy{
				FocusableWhenEnabled: !p.Disabled,
			},
			Layout: core.LayoutPolicy{
				Width:  core.Fit(),
				Height: core.Fit(),
			},
		})
}
