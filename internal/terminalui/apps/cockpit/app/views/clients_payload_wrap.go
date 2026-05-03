package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
)

const (
	// Keep payload disclosure readable in minimum viewport while still bounded
	// on wider terminals.
	clientPayloadWrapWidth = 50
)

func payloadTextRow(text string) view.ViewSpec[state.Model] {
	return IndentLeft[state.Model](view.FromRenderNode[state.Model](toolkitviews.NewAction(toolkitviews.RuneLen(text), false, false, func(_ bool, width int) string {
		return toolkitviews.PadRight(toolkitviews.TrimToWidth(text, width), width)
	}, nil, nil)), InsetSection+InsetDetail)
}

func wrappedPayloadTextRows(line string) []view.ViewSpec[state.Model] {
	return toolkitviews.WrapLineRowsPreserveIndent(line, clientPayloadWrapWidth, payloadTextRow)
}
