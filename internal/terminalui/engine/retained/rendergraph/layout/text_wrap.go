package layout

import "strings"

// WrapLinePreserveIndent wraps long text while preserving leading indentation.
// Blank lines return nil so callers can skip rendering empty rows.
func WrapLinePreserveIndent(line string, width int) []string {
	line = strings.TrimRight(line, " \t")
	if strings.TrimSpace(line) == "" {
		return nil
	}
	if width <= 0 || len([]rune(line)) <= width {
		return []string{line}
	}
	leading := line[:len(line)-len(strings.TrimLeft(line, " "))]
	content := strings.TrimLeft(line, " ")
	if strings.TrimSpace(content) == "" {
		return []string{line}
	}
	usable := width - len([]rune(leading))
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
				if len([]rune(remaining)) <= usable {
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
			if len([]rune(candidate)) <= usable {
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
	runes := []rune(value)
	if len(runes) <= width {
		return []string{value}
	}
	chunks := make([]string, 0, len(runes)/width+1)
	for len(runes) > width {
		chunks = append(chunks, string(runes[:width]))
		runes = runes[width:]
	}
	if len(runes) > 0 {
		chunks = append(chunks, string(runes))
	}
	return chunks
}
