package primitive

import "github.com/swobuforge/swobu/internal/terminalui/core"

// InputProps describes one controlled semantic input field.
//
// Label and EmptyValue are kept in the API for the eventual compound wrapper
// and cockpit migration path; the primitive leaf currently carries the value
// and semantic signal contract.
type InputProps struct {
	Key        core.Key
	Label      string
	Value      string
	EmptyValue string
	OnChange   core.Signal
	OnCommit   core.Signal
	OnCancel   core.Signal
}

// Input returns one focusable semantic input leaf with change/commit/cancel
// signal slots.
func Input(p InputProps) core.Node {
	help := []core.HelpBinding{
		{Key: "↵", Label: "save"},
		{Key: "esc", Label: "cancel"},
	}
	node := core.Input(p.Value).
		Key(p.Key).
		Style(core.Style{Token: core.TokenTextDefault}).
		Interaction(core.Interaction{
			Focus:   core.FocusSpec{Mode: core.Focusable},
			Signals: []core.Signal{p.OnChange, p.OnCommit, p.OnCancel},
			Help:    help,
		}).
		Contract(core.Contract{
			Name:    "Input",
			Purpose: "Focusable semantic input leaf.",
			Signals: []core.SignalSpec{
				{Kind: p.OnChange.Kind},
				{Kind: p.OnCommit.Kind},
				{Kind: p.OnCancel.Kind},
			},
			Focus: core.FocusGuarantee{FocusableWhenEnabled: true},
			Layout: core.LayoutGuarantee{
				Width:  core.Fill(1),
				Height: core.Fit(),
			},
			Help: help,
		})
	if p.Label != "" {
		node = node.Debug(core.Debug{Name: p.Label})
	}
	return node
}
