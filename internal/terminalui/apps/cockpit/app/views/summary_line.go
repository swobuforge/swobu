package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// SummaryRow renders a non-focusable summary line directly under a section
// title without key/value column alignment.
func SummaryRow(value string) retained.ViewSpec[state.Model] {
	text := strings.TrimSpace(value) // swobu:io-string source=boundary
	return IndentLeft[state.Model](StaticTextLine[state.Model](text), InsetDetail)
}
