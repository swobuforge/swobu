package views

import "strings"

// RenderSplitLine renders one line with left/right content separated by a
// single space, truncating the left side first when width is constrained.
func RenderSplitLine(width int, left string, right string) string {
	left = strings.TrimSpace(left)
	if width <= 0 {
		return ""
	}
	leftWidth := width - runeLen(right) - 1
	if leftWidth < 0 {
		leftWidth = 0
	}
	return padRight(trimToWidth(left, leftWidth), leftWidth) + " " + trimToWidth(right, max(0, width-leftWidth-1))
}
