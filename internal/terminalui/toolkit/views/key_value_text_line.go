package views

import "strings"

// FormatKeyValueTextLine formats a non-focusable key/value line with fixed key width.
func FormatKeyValueTextLine(key string, value string, keyWidth int) string {
	k := strings.TrimSpace(key)   // swobu:io-string source=boundary
	v := strings.TrimSpace(value) // swobu:io-string source=boundary
	if keyWidth < 1 {
		keyWidth = 1
	}
	line := PadRight(TrimToWidth(k, keyWidth), keyWidth)
	if v != "" {
		line += " " + v
	}
	return strings.TrimRight(line, " ")
}
