package views

import (
	"fmt"
	"strings"

	"github.com/metrofun/swobu/internal/terminalui/engine/model"
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

func MessageBlock(title string, rows []string, wrapWidth int) model.Node {
	if wrapWidth < 20 {
		wrapWidth = 72
	}
	children := []model.Node{Line(fmt.Sprintf("== %s ==", strings.TrimSpace(title)))}
	for _, row := range rows {
		for _, wrapped := range Wrap(row, wrapWidth) {
			children = append(children, Line("- "+wrapped))
		}
	}
	return model.Node{Kind: "message_block", Children: children}
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
