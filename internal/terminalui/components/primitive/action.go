package primitive

import "github.com/swobuforge/swobu/internal/terminalui/core"

// ActionProps describes one focusable semantic action row and its emitted
// activation and focus signals.
type ActionProps struct {
	Key    core.Key
	Label  string
	Signal core.SignalEvent
	// FocusSignal is emitted when the action gains focus.
	FocusSignal core.SignalEvent
	Disabled    bool
	Help        []core.HelpBindingSpec
}

// Action returns one semantic action node with enter binding and optional
// activation/focus signals.
func Action(p ActionProps) core.Node {
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

	signals := make([]core.SignalEvent, 0, 2)
	contractSignals := make([]core.SignalSpec, 0, 2)
	if p.Signal.Kind != "" {
		signals = append(signals, p.Signal)
		contractSignals = append(contractSignals, core.SignalSpec{Kind: p.Signal.Kind})
	}

	return core.Action(p.Label, p.Signal).
		Key(p.Key).
		Style(core.Style{Token: token, State: state}).
		Interaction(core.InteractionSpec{
			Focus:   core.FocusSpec{Mode: focusMode},
			Keymap:  []core.KeyBindingSpec{{Pattern: core.KeyEnter(), Intent: core.IntentActivate}},
			Help:    help,
			Signals: signals,
			FocusSignals: func() []core.SignalEvent {
				if p.FocusSignal.Kind == "" {
					return nil
				}
				return []core.SignalEvent{p.FocusSignal}
			}(),
		}).
		Contract(core.Contract{
			Name:    "Action",
			Purpose: "Focusable semantic action.",
			Signals: contractSignals,
			Help:    help,
			Focus: core.FocusPolicy{
				FocusableWhenEnabled: !p.Disabled,
			},
			Layout: core.LayoutPolicy{
				Width:  core.Fill(1),
				Height: core.Fit(),
			},
		})
}
