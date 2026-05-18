package layout

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/view/textmetrics"
)

// WrapLinePreserveIndent wraps long text while preserving leading indentation.
// Blank lines return nil so callers can skip rendering empty rows.
func WrapLinePreserveIndent(line string, width int) []string {
	line = strings.TrimRight(line, " \t")
	if strings.TrimSpace(line) == "" { // swobu:io-string source=boundary
		return nil
	}
	if width <= 0 || textmetrics.Width(line) <= width {
		return []string{line}
	}
	leading := line[:len(line)-len(strings.TrimLeft(line, " "))]
	content := strings.TrimLeft(line, " ")
	if strings.TrimSpace(content) == "" { // swobu:io-string source=boundary
		return []string{line}
	}
	usable := width - textmetrics.Width(leading)
	if usable < 12 {
		usable = width
		leading = ""
	}
	words := strings.Fields(content)
	if len(words) == 0 {
		return []string{line}
	}
	lines := make([]string, 0, 4)
	current := ""
	for _, word := range words {
		remaining := word
		for remaining != "" {
			if current == "" {
				if textmetrics.Width(remaining) <= usable {
					current = remaining
					remaining = ""
					continue
				}
				chunks := splitByRuneWidth(remaining, usable)
				if len(chunks) == 0 {
					break
				}
				for _, chunk := range chunks[:len(chunks)-1] {
					lines = append(lines, leading+chunk)
				}
				current = chunks[len(chunks)-1]
				remaining = ""
				continue
			}
			candidate := current + " " + remaining
			if textmetrics.Width(candidate) <= usable {
				current = candidate
				remaining = ""
				continue
			}
			lines = append(lines, leading+current)
			current = ""
		}
	}
	if current != "" {
		lines = append(lines, leading+current)
	}
	return lines
}

func splitByRuneWidth(value string, width int) []string {
	if width <= 0 {
		return []string{value}
	}
	if textmetrics.Width(value) <= width {
		return []string{value}
	}
	chunks := make([]string, 0, 4)
	var b strings.Builder
	cells := 0
	for _, r := range value {
		rw := textmetrics.Width(string(r))
		if rw <= 0 {
			rw = 1
		}
		if cells+rw > width && b.Len() > 0 {
			chunks = append(chunks, b.String())
			b.Reset()
			cells = 0
		}
		b.WriteRune(r)
		cells += rw
	}
	if b.Len() > 0 {
		chunks = append(chunks, b.String())
	}
	return chunks
}
