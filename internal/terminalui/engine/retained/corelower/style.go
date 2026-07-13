package corelower

import (
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

// StyleResolver maps semantic style tokens to concrete terminal paint styles.
//
// This is a map-based resolver, not a full CSS cascade engine. TUI color
// palettes are limited; a direct mapping is sufficient for the current 16
// semantic tokens.
type StyleResolver struct {
	ColorConfig ColorConfig
}

// ColorConfig defines the concrete terminal colors for one theme.
type ColorConfig struct {
	// Foreground colors (ANSI16)
	FgDefault paint.Color
	FgMuted   paint.Color
	FgDanger  paint.Color
	FgSuccess paint.Color
	FgPrimary paint.Color

	// Background colors (ANSI16)
	BgDefault  paint.Color
	BgSelected paint.Color

	// Border colors
	BorderDefault paint.Color
	BorderFocused paint.Color
}

// DefaultColorConfig is the default 16-color ANSI palette used by the
// corelower bridge when no custom palette is supplied.
var DefaultColorConfig = ColorConfig{
	FgDefault:     paint.ColorWhite,
	FgMuted:       paint.ColorWhite,
	FgDanger:      paint.ColorRed,
	FgSuccess:     paint.ColorGreen,
	FgPrimary:     paint.ColorCyan,
	BgDefault:     paint.ColorDefault,
	BgSelected:    paint.ColorBlue,
	BorderDefault: paint.ColorWhite,
	BorderFocused: paint.ColorCyan,
}

// Resolve maps one semantic Style to a concrete paint.Style using the
// resolver's palette. VisualState modifiers override or blend the base token.
func (r StyleResolver) Resolve(style core.Style) paint.Style {
	ps := paint.Style{}

	// Base token color.
	switch style.Token {
	case core.TokenTextDefault:
		ps.Fg = r.ColorConfig.FgDefault
	case core.TokenTextMuted:
		ps.Fg = r.ColorConfig.FgMuted
		ps.Dim = true
	case core.TokenTextDanger:
		ps.Fg = r.ColorConfig.FgDanger
	case core.TokenTextSuccess:
		ps.Fg = r.ColorConfig.FgSuccess
	case core.TokenAccentPrimary:
		ps.Fg = r.ColorConfig.FgPrimary
	case core.TokenSurfaceSelected:
		ps.Bg = r.ColorConfig.BgSelected
	case core.TokenSurfaceDefault:
		ps.Bg = r.ColorConfig.BgDefault
	case core.TokenBorderDefault:
		ps.Fg = r.ColorConfig.BorderDefault
	case core.TokenBorderFocused:
		ps.Fg = r.ColorConfig.BorderFocused
	}

	// State overrides.
	switch style.State {
	case core.StateFocused:
		ps.Bold = true
		if style.Token == core.TokenBorderDefault || style.Token == "" {
			ps.Fg = r.ColorConfig.BorderFocused
		}
	case core.StateSelected:
		ps.Bg = r.ColorConfig.BgSelected
		ps.Bold = true
	case core.StateDisabled:
		ps.Dim = true
	case core.StateDanger:
		ps.Fg = r.ColorConfig.FgDanger
	case core.StateSuccess:
		ps.Fg = r.ColorConfig.FgSuccess
	}

	// Explicit modifiers win over derived state.
	ps.Bold = overrideTri(style.Mods.Bold, ps.Bold)
	ps.Dim = overrideTri(style.Mods.Dim, ps.Dim)
	ps.Underline = overrideTri(style.Mods.Underline, ps.Underline)

	return ps
}

func overrideTri(tri core.Tri, current bool) bool {
	switch tri {
	case core.TriTrue:
		return true
	case core.TriFalse:
		return false
	default:
		return current
	}
}
