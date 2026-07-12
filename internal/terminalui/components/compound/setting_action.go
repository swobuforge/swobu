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
	Signal      core.SignalEvent
	// FocusSignal is emitted when the row gains focus.
	FocusSignal core.SignalEvent
	Disabled    bool
	Selected    bool
	Help        []core.HelpBindingSpec
}

// SettingRow returns one semantic action row rendered as a single focusable
// action node. It keeps the layout bridge narrow while the retained core
// adapter is still migrating.
func SettingRow(p SettingRowProps) core.Node {
	label := strings.TrimSpace(p.Label)             // swobu:io-string source=boundary
	value := strings.TrimSpace(p.Value)             // swobu:io-string source=boundary
	actionLabel := strings.TrimSpace(p.ActionLabel) // swobu:io-string source=boundary
	line := strings.Join(filterParts(label, value, actionLabel), "  ")

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
