package views

import (
	"strings"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
	toolkitviews "github.com/metrofun/swobu/internal/adapters/inbound/tui/toolkit/views"
)

// StaticTextLine renders one non-focusable line.
func StaticTextLine[M any](text string) view.ViewSpec[M] {
	text = strings.TrimSpace(text)
	return view.FromRenderNode[M](toolkitviews.NewAction(toolkitviews.RuneLen(text), false, false, func(_ bool, width int) string {
		return toolkitviews.PadRight(toolkitviews.TrimToWidth(text, width), width)
	}, nil, nil))
}

// IndentLeft composes a view under a parent-owned left inset.
func IndentLeft[M any](child view.ViewSpec[M], cols int) view.ViewSpec[M] {
	if cols <= 0 {
		return child
	}
	return view.WithPadLeft[M](cols)(child)
}
