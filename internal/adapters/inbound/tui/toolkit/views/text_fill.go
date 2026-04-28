package views

import (
	"strings"
	"unicode/utf8"
)

func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	n := runeLen(s)
	if n >= width {
		return trimToWidth(s, width)
	}
	return s + strings.Repeat(" ", width-n)
}

// PadRight right-pads or truncates text to width using toolkit grammar.
func PadRight(s string, width int) string {
	return padRight(s, width)
}

func trimToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runeLen(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count >= width-1 {
			break
		}
		b.WriteRune(r)
		count++
	}
	b.WriteRune('…')
	return b.String()
}

// TrimToWidth trims text to width with ellipsis when needed.
func TrimToWidth(s string, width int) string {
	return trimToWidth(s, width)
}

// trimToWidthRaw truncates a string to at most `width` runes without adding
// an ellipsis. Callers that want visual truncation indicators should use
// trimToWidth instead.
func trimToWidthRaw(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width])
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
	return utf8.RuneCountInString(s)
}

// RuneLen returns rune-aware text width.
func RuneLen(s string) int {
	return runeLen(s)
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
