package primitive

import "github.com/swobuforge/swobu/internal/terminalui/core"

// ActionProps describes one focusable semantic action row and its emitted
// activation and focus signals.
type ActionProps struct {
	Key    core.Key
	Label  string
	Signal core.Signal
	// FocusSignal is emitted when the action gains focus.
	FocusSignal core.Signal
	Disabled    bool
	Help        []core.HelpBinding
}

// Action returns one semantic action node with enter binding and optional
// activation/focus signals.
func Action(p ActionProps) core.Node {
	help := p.Help
	if len(help) == 0 {
		help = []core.HelpBinding{{Key: "enter", Label: "activate"}}
	}

	state := core.StateDefault
	token := core.TokenTextDefault
	focusMode := core.Focusable
	if p.Disabled {
		state = core.StateDisabled
		token = core.TokenTextMuted
		focusMode = core.FocusNone
	}

	signals := make([]core.Signal, 0, 2)
	contractSignals := make([]core.SignalSpec, 0, 2)
	if p.Signal.Kind != "" {
		signals = append(signals, p.Signal)
		contractSignals = append(contractSignals, core.SignalSpec{Kind: p.Signal.Kind})
	}

	return core.Action(p.Label, p.Signal).
		Key(p.Key).
		Style(core.Style{Token: token, State: state}).
		Interaction(core.Interaction{
			Focus:   core.FocusSpec{Mode: focusMode},
			Keymap:  []core.KeyBinding{{Pattern: core.KeyEnter(), Intent: core.IntentActivate}},
			Help:    help,
			Signals: signals,
			FocusSignals: func() []core.Signal {
				if p.FocusSignal.Kind == "" {
					return nil
				}
				return []core.Signal{p.FocusSignal}
			}(),
		}).
		Contract(core.Contract{
			Name:    "Action",
			Purpose: "Focusable semantic action.",
			Signals: contractSignals,
			Help:    help,
			Focus: core.FocusGuarantee{
				FocusableWhenEnabled: !p.Disabled,
			},
			Layout: core.LayoutGuarantee{
				Width:  core.Fill(1),
				Height: core.Fit(),
			},
		})
}
