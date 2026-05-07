package views

import (
	"strings"

	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// StaticTextLine renders one non-focusable line.
func StaticTextLine[M any](text string) retained.ViewSpec[M] {
	text = strings.TrimSpace(text)
	return retained.FromRenderNode[M](toolkitviews.NewAction(toolkitviews.RuneLen(text), false, false, func(_ bool, width int) string {
		return toolkitviews.PadRight(toolkitviews.TrimToWidth(text, width), width)
	}, nil, nil))
}

// IndentLeft composes a view under a parent-owned left inset.
func IndentLeft[M any](child retained.ViewSpec[M], cols int) retained.ViewSpec[M] {
	if cols <= 0 {
		return child
	}
	return retained.WithPadLeft[M](cols)(child)
}
