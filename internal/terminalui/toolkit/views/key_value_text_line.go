package views

import "strings"

// FormatKeyValueTextLine formats a non-focusable key/value line with fixed key width.
func FormatKeyValueTextLine(key string, value string, keyWidth int) string {
	k := strings.TrimSpace(key)
	v := strings.TrimSpace(value)
	if keyWidth < 1 {
		keyWidth = 1
	}
	line := PadRight(TrimToWidth(k, keyWidth), keyWidth)
	if v != "" {
		line += " " + v
	}
	return strings.TrimRight(line, " ")
}

// RenderKeyValueTextLine formats and clips a key/value line to width.
func RenderKeyValueTextLine(width int, key string, value string, keyWidth int) string {
	if width <= 0 {
		return ""
	}
	line := FormatKeyValueTextLine(key, value, keyWidth)
	return PadRight(TrimToWidth(line, width), width)
}
