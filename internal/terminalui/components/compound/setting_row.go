package compound

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/components/primitive"
	"github.com/swobuforge/swobu/internal/terminalui/core"
)

// SettingRowProps describes one focusable key/value/action line and its
// optional focus affordance signal.
type SettingRowProps struct {
	Key         core.Key
	Label       string
	Value       string
	ActionLabel string
	Signal      core.Signal
	// FocusSignal is emitted when the row gains focus.
	FocusSignal core.Signal
	Disabled    bool
	Selected    bool
	Help        []core.HelpBinding
}

// SettingRow returns one semantic action row rendered as a single focusable
// action node. It keeps the layout bridge narrow while the retained core
// adapter is still migrating.
func SettingRow(p SettingRowProps) core.Node {
	line := strings.Join(filterParts(
		strings.TrimSpace(p.Label),
		strings.TrimSpace(p.Value),
		strings.TrimSpace(p.ActionLabel),
	), "  ")

	row := primitive.Action(primitive.ActionProps{
		Key:         p.Key,
		Label:       line,
		Signal:      p.Signal,
		FocusSignal: p.FocusSignal,
		Disabled:    p.Disabled,
		Help:        p.Help,
	})

	style := core.Style{Token: core.TokenTextDefault, State: core.StateDefault}
	if p.Selected {
		style = core.Style{Token: core.TokenSurfaceSelected, State: core.StateSelected}
	} else if p.Disabled {
		style = core.Style{Token: core.TokenTextMuted, State: core.StateDisabled}
	}
	return row.Style(style)
}

func filterParts(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
