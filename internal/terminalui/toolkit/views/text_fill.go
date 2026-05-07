package views

import (
	"unicode/utf8"

	"github.com/swobuforge/swobu/internal/terminalui/view/textmetrics"
)

func padRight(s string, width int) string {
	return textmetrics.PadRight(s, width)
}

// PadRight right-pads or truncates text to width using toolkit grammar.
func PadRight(s string, width int) string {
	return padRight(s, width)
}

func trimToWidth(s string, width int) string {
	return textmetrics.TrimToWidth(s, width)
}

// TrimToWidth trims text to width with ellipsis when needed.
func TrimToWidth(s string, width int) string {
	return trimToWidth(s, width)
}

// trimToWidthRaw truncates a string to at most `width` runes without adding
// an ellipsis. Callers that want visual truncation indicators should use
// trimToWidth instead.
func trimToWidthRaw(s string, width int) string {
	return textmetrics.TrimToWidthRaw(s, width)
}

// trimLastRune removes the last rune from s.
func trimLastRune(value string) string {
	if value == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(value)
	if size <= 0 {
		return ""
	}
	return value[:len(value)-size]
}

func runeLen(s string) int {
	return textmetrics.Width(s)
}

// RuneLen returns rune-aware text width.
func RuneLen(s string) int {
	return runeLen(textmetrics.SanitizeTerminalText(s))
}

func sanitizeTextForTerminal(s string) string {
	return textmetrics.SanitizeTerminalText(s)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// maxInt returns the larger of a or b.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
