package views

import (
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
	toolkitviews "github.com/metrofun/swobu/internal/adapters/inbound/tui/toolkit/views"
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
