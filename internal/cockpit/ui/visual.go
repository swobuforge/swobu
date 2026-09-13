package ui

import tui "github.com/grindlemire/go-tui"

// Tone names terminal-facing meaning rather than a particular pigment.
// Producers choose the tone from structured UI state; this package alone maps
// it onto the Cockpit palette.
type Tone uint8

const (
	ToneNeutral Tone = iota
	ToneAccent
	ToneMuted
	ToneFallback
	ToneWarning
	ToneFailure
)

var (
	accentColor   = tui.RGBColor(70, 167, 88)
	mutedColor    = tui.BrightBlack
	fallbackColor = tui.RGBColor(247, 107, 21)
	warningColor  = tui.Yellow
	failureColor  = tui.RGBColor(229, 72, 77)
	onAccentColor = tui.RGBColor(18, 25, 18)
)

func toneStyle(tone Tone) tui.Style {
	style := tui.NewStyle()
	switch tone {
	case ToneAccent:
		return style.Foreground(accentColor).Bold()
	case ToneMuted:
		return style.Foreground(mutedColor)
	case ToneFallback:
		return style.Foreground(fallbackColor).Bold()
	case ToneWarning:
		return style.Foreground(warningColor).Bold()
	case ToneFailure:
		return style.Foreground(failureColor).Bold()
	default:
		return style
	}
}

func selectedMarkerStyle() tui.Style {
	return toneStyle(ToneAccent)
}

func activeFieldStyle() tui.Style { return tui.NewStyle().Bold() }

func activeActionStyle() tui.Style {
	return toneStyle(ToneAccent)
}

func headingStyle() tui.Style { return tui.NewStyle().Bold() }

func activeTabStyle() tui.Style {
	// Reverse is semantic, not an environment fallback. When color capability
	// strips the palette this remains bold+reverse and the active tab stays clear.
	return tui.NewStyle().Foreground(accentColor).Background(onAccentColor).Bold().Reverse()
}

// Exported style helpers are used only by composition that cannot name a Tone.
// Feature code should prefer Tone so palette values remain centralized.
func ActiveTabStyle() tui.Style     { return activeTabStyle() }
func HeadingStyle() tui.Style       { return headingStyle() }
func ToneStyle(tone Tone) tui.Style { return toneStyle(tone) }
