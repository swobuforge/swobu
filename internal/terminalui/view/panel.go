package view

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/view/textmetrics"
)

type PanelBorderStyle struct {
	TopLeft      string
	TopRight     string
	BottomLeft   string
	BottomRight  string
	Horizontal   string
	Vertical     string
	TitlePrefix  string
	TitleSuffix  string
	FallbackName string
}

type PanelSpec struct {
	Title       string
	Rows        []string
	TargetWidth int
	MinWidth    int
	MaxWidth    int
	PadLeft     int
	PadRight    int
	Border      PanelBorderStyle
}

func DurablePanel(spec PanelSpec) ViewSpec {
	cloned := spec
	cloned.Rows = append([]string(nil), spec.Rows...)
	return ViewSpec{Kind: "panel", Retention: RetentionDurable, Panel: &cloned}
}

func defaultPanelBorderStyle() PanelBorderStyle {
	return PanelBorderStyle{
		TopLeft:      "╭",
		TopRight:     "╮",
		BottomLeft:   "╰",
		BottomRight:  "╯",
		Horizontal:   "─",
		Vertical:     "│",
		TitlePrefix:  "─ ",
		TitleSuffix:  " ",
		FallbackName: "Box",
	}
}

func renderPanelLines(spec PanelSpec) []string {
	if spec.Border.TopLeft == "" || spec.Border.TopRight == "" || spec.Border.BottomLeft == "" || spec.Border.BottomRight == "" || spec.Border.Horizontal == "" || spec.Border.Vertical == "" {
		spec.Border = defaultPanelBorderStyle()
	}
	width := spec.TargetWidth
	if width <= 0 {
		width = spec.MinWidth
	}
	if spec.MinWidth > 0 && width < spec.MinWidth {
		width = spec.MinWidth
	}
	if spec.MaxWidth > 0 && width > spec.MaxWidth {
		width = spec.MaxWidth
	}
	if width < 4 {
		width = 4
	}
	leftPad := max(0, spec.PadLeft)
	rightPad := max(0, spec.PadRight)
	innerWidth := width - 2
	contentWidth := innerWidth - leftPad - rightPad
	if contentWidth < 1 {
		contentWidth = 1
	}
	out := []string{renderPanelTop(spec.Title, innerWidth, spec.Border)}
	for _, row := range spec.Rows {
		wrapped := wrapText(strings.TrimSpace(row), contentWidth) // trimlowerlint:allow boundary canonicalization
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for _, line := range wrapped {
			body := padRight(trimToWidth(line, contentWidth), contentWidth)
			out = append(out, spec.Border.Vertical+strings.Repeat(" ", leftPad)+body+strings.Repeat(" ", rightPad)+spec.Border.Vertical)
		}
	}
	out = append(out, spec.Border.BottomLeft+strings.Repeat(spec.Border.Horizontal, innerWidth)+spec.Border.BottomRight)
	return out
}

func renderPanelTop(title string, innerWidth int, border PanelBorderStyle) string {
	name := strings.TrimSpace(title) // trimlowerlint:allow boundary canonicalization
	if name == "" {
		name = strings.TrimSpace(border.FallbackName) // trimlowerlint:allow boundary canonicalization
		if name == "" {
			name = "Box"
		}
	}
	label := border.TitlePrefix + name + border.TitleSuffix
	if runeLen(label) > innerWidth-1 {
		limit := innerWidth - runeLen(border.TitlePrefix+border.TitleSuffix)
		if limit < 1 {
			limit = 1
		}
		label = border.TitlePrefix + trimToWidth(name, limit) + border.TitleSuffix
	}
	remaining := innerWidth - runeLen(label)
	if remaining < 0 {
		remaining = 0
	}
	return border.TopLeft + label + strings.Repeat(border.Horizontal, remaining) + border.TopRight
}

func wrapText(text string, width int) []string {
	if text == "" {
		return nil
	}
	if width <= 0 || runeLen(text) <= width {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	out := make([]string, 0, 2)
	line := words[0]
	for _, w := range words[1:] {
		next := line + " " + w
		if runeLen(next) <= width {
			line = next
			continue
		}
		out = append(out, line)
		line = w
	}
	out = append(out, line)
	return out
}

func trimToWidth(s string, width int) string {
	return textmetrics.TrimToWidth(s, width)
}

func padRight(s string, width int) string {
	return textmetrics.PadRight(s, width)
}

func runeLen(s string) int { return textmetrics.Width(s) }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
