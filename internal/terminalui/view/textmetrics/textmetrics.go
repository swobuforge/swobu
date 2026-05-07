package textmetrics

import (
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

func Width(s string) int { return runewidth.StringWidth(s) }

func SanitizeTerminalText(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(' ')
		case r == 0x1b:
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TrimToWidthRaw(s string, width int) string {
	s = SanitizeTerminalText(s)
	if width <= 0 {
		return ""
	}
	if Width(s) <= width {
		return s
	}
	var b strings.Builder
	cells := 0
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if w < 0 {
			w = 0
		}
		if cells+w > width {
			break
		}
		b.WriteRune(r)
		cells += w
	}
	return b.String()
}

func TrimToWidth(s string, width int) string {
	s = SanitizeTerminalText(s)
	if width <= 0 {
		return ""
	}
	if Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	var b strings.Builder
	cells := 0
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if w < 0 {
			w = 0
		}
		if cells+w > width-1 {
			break
		}
		b.WriteRune(r)
		cells += w
	}
	b.WriteRune('…')
	return b.String()
}

func PadRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	n := Width(s)
	if n >= width {
		return TrimToWidth(s, width)
	}
	return s + strings.Repeat(" ", width-n)
}
