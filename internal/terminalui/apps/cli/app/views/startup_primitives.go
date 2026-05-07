package views

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/model"
)

func Line(text string) model.Node {
	return model.Node{Kind: "line", Text: text, Durability: model.Durable}
}

func Status(text string) model.Node {
	return model.Node{Kind: "status", Text: text, Durability: model.Ephemeral}
}

func Block(title string, rows ...string) model.Node {
	children := make([]model.Node, 0, len(rows)+1)
	children = append(children, Line(title))
	for _, row := range rows {
		children = append(children, Line(row))
	}
	return model.Node{Kind: "block", Children: children}
}

func Frame(title string, subtitle string, width int) model.Node {
	if width < 10 {
		width = 34
	}
	border := "+" + strings.Repeat("-", width-2) + "+"
	rows := []string{
		border,
		"|" + Center(title, width-2) + "|",
		"|" + Center(subtitle, width-2) + "|",
		border,
	}
	children := make([]model.Node, 0, len(rows))
	for _, row := range rows {
		children = append(children, Line(row))
	}
	return model.Node{Kind: "frame", Children: children}
}

func SplashBlock(rows []string) model.Node {
	children := make([]model.Node, 0, len(rows))
	for _, row := range rows {
		children = append(children, Line(row))
	}
	return model.Node{Kind: "splash", Children: children}
}

type TextPanelBorderStyle struct {
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

type TextPanelSpec struct {
	Title         string
	Body          []string
	TargetWidth   int
	MinWidth      int
	MaxWidth      int
	HorizontalPad int
	Border        TextPanelBorderStyle
}

type PanelContentView func(contentWidth int) []string

func defaultMessagePanelBorderStyle() TextPanelBorderStyle {
	return TextPanelBorderStyle{
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

func defaultMessagePanelSpec(title string, rows []string, wrapWidth int) TextPanelSpec {
	if wrapWidth < 20 {
		wrapWidth = 72
	}
	return TextPanelSpec{
		Title:         strings.TrimSpace(title),
		Body:          rows,
		TargetWidth:   wrapWidth + 4,
		MinWidth:      20,
		MaxWidth:      0,
		HorizontalPad: 1,
		Border:        defaultMessagePanelBorderStyle(),
	}
}

func MessageBlock(title string, rows []string, wrapWidth int) model.Node {
	spec := defaultMessagePanelSpec(title, rows, wrapWidth)
	content := WrappedTextRowsView(rows)
	lines := RenderTextPanel(spec, content)
	children := make([]model.Node, 0, len(lines))
	for _, line := range lines {
		children = append(children, Line(line))
	}
	return model.Node{Kind: "message_block", Children: children}
}

func RenderTextPanel(spec TextPanelSpec, content PanelContentView) []string {
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

	pad := spec.HorizontalPad
	if pad < 0 {
		pad = 0
	}

	innerWidth := width - 2
	contentWidth := innerWidth - (pad * 2)
	if contentWidth < 1 {
		contentWidth = 1
	}

	top := renderTextPanelTop(spec.Title, innerWidth, spec.Border)
	lines := []string{top}
	if content != nil {
		for _, row := range content(contentWidth) {
			lines = append(lines, renderTextPanelBodyLine(row, contentWidth, pad, spec.Border))
		}
	}
	lines = append(lines, spec.Border.BottomLeft+strings.Repeat(spec.Border.Horizontal, innerWidth)+spec.Border.BottomRight)
	return lines
}

func WrappedTextRowsView(rows []string) PanelContentView {
	captured := append([]string(nil), rows...)
	return func(contentWidth int) []string {
		out := make([]string, 0, len(captured))
		for _, raw := range captured {
			wrapped := Wrap(strings.TrimSpace(raw), contentWidth)
			if len(wrapped) == 0 {
				out = append(out, "")
				continue
			}
			out = append(out, wrapped...)
		}
		return out
	}
}

func StackPanelContentViews(views ...PanelContentView) PanelContentView {
	captured := append([]PanelContentView(nil), views...)
	return func(contentWidth int) []string {
		out := make([]string, 0)
		for _, v := range captured {
			if v == nil {
				continue
			}
			out = append(out, v(contentWidth)...)
		}
		return out
	}
}

func renderTextPanelTop(title string, innerWidth int, border TextPanelBorderStyle) string {
	if border.TopLeft == "" || border.TopRight == "" || border.BottomLeft == "" || border.BottomRight == "" || border.Horizontal == "" || border.Vertical == "" {
		border = defaultMessagePanelBorderStyle()
	}
	name := strings.TrimSpace(title)
	if name == "" {
		name = border.FallbackName
		if strings.TrimSpace(name) == "" {
			name = "Box"
		}
	}
	label := border.TitlePrefix + name + border.TitleSuffix
	if title != "" {
		label = border.TitlePrefix + name + border.TitleSuffix
	}
	if len([]rune(label)) > innerWidth-1 {
		titleRunes := []rune(name)
		limit := innerWidth - len([]rune(border.TitlePrefix+border.TitleSuffix))
		if limit < 1 {
			limit = 1
		}
		if len(titleRunes) > limit {
			titleRunes = titleRunes[:limit]
		}
		label = border.TitlePrefix + string(titleRunes) + border.TitleSuffix
	}
	remaining := innerWidth - len([]rune(label))
	if remaining < 0 {
		remaining = 0
	}
	return border.TopLeft + label + strings.Repeat(border.Horizontal, remaining) + border.TopRight
}

func renderTextPanelBodyLine(text string, contentWidth int, horizontalPad int, border TextPanelBorderStyle) string {
	content := text
	r := []rune(content)
	if len(r) > contentWidth {
		content = string(r[:contentWidth])
	}
	padding := contentWidth - len([]rune(content))
	if padding < 0 {
		padding = 0
	}
	content = content + strings.Repeat(" ", padding)
	sidePad := strings.Repeat(" ", horizontalPad)
	return border.Vertical + sidePad + content + sidePad + border.Vertical
}

func StatusLine(phase string, message string) model.Node {
	line := fmt.Sprintf("[%s] %s", strings.ToUpper(strings.TrimSpace(phase)), strings.TrimSpace(message))
	return Line(line)
}

func Center(text string, width int) string {
	t := strings.TrimSpace(text)
	if width <= 0 {
		return t
	}
	if len(t) >= width {
		return t[:width]
	}
	pad := width - len(t)
	left := pad / 2
	right := pad - left
	return strings.Repeat(" ", left) + t + strings.Repeat(" ", right)
}

func Wrap(text string, width int) []string {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}
	if width <= 0 || len(t) <= width {
		return []string{t}
	}
	words := strings.Fields(t)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0)
	line := words[0]
	for _, w := range words[1:] {
		candidate := line + " " + w
		if len(candidate) <= width {
			line = candidate
			continue
		}
		lines = append(lines, line)
		line = w
	}
	lines = append(lines, line)
	return lines
}
