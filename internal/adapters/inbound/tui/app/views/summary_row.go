package views

import (
	"strings"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
)

// SummaryRow renders a non-focusable summary line directly under a section
// title without key/value column alignment.
func SummaryRow(value string) view.ViewSpec[state.Model] {
	text := strings.TrimSpace(value)
	return IndentLeft[state.Model](StaticTextLine[state.Model](text), InsetDetail)
}
