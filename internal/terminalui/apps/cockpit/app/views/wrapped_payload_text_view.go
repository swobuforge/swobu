package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

const (
	// Keep payload disclosure readable in minimum viewport while still bounded
	// on wider terminals.
	clientPayloadWrapWidth = 50
)

func payloadTextRow(text string) retained.ViewSpec[state.Model] {
	return IndentLeft[state.Model](retained.FromRenderNode[state.Model](toolkitviews.NewAction(toolkitviews.RuneLen(text), false, false, func(_ bool, width int) string {
		return toolkitviews.PadRight(toolkitviews.TrimToWidthRaw(text, width), width)
	}, nil, nil)), InsetSection+InsetDetail)
}

func wrappedPayloadTextRows(line string) []retained.ViewSpec[state.Model] {
	return toolkitviews.WrapLineRowsPreserveIndent(line, clientPayloadWrapWidth, payloadTextRow)
}
