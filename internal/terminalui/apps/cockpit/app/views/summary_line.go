package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// SummaryRowNode returns one summary helper line as a core.Node.
func SummaryRowNode(value string) core.Node[state.Action] {
	value = strings.TrimSpace(value) // swobu:io-string source=boundary
	return IndentLeftNode(InsetSection)(core.Text[state.Action](value))
}

// SummaryRow returns one summary helper line as a retained.ViewSpec.
// Deprecated: use SummaryRowNode for new code.
func SummaryRow(value string) retained.ViewSpec[state.Model] {
	return CoreNodeAsRetained(SummaryRowNode(value))
}
