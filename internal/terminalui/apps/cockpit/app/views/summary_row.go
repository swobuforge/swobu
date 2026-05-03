package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
)

// SummaryRow renders a non-focusable summary line directly under a section
// title without key/value column alignment.
func SummaryRow(value string) view.ViewSpec[state.Model] {
	text := strings.TrimSpace(value)
	return IndentLeft[state.Model](StaticTextLine[state.Model](text), InsetDetail)
}
