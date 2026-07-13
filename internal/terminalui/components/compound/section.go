package compound

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/core"
)

// SectionNode composes a titled vertical stack of semantic row nodes.
// It is the pure-core counterpart to the retained view Section helper.
func SectionNode[E any](title string, rows ...core.Node[E]) core.Node[E] {
	children := make([]core.Node[E], 0, len(rows)+1)
	header := sectionHeader[E](title)
	children = append(children, header)
	children = append(children, rows...)
	return core.Stack[E](core.AxisVertical, children...).
		Layout(core.Layout{
			Size: core.Size{Width: core.Fill(1), Height: core.Fit()},
		})
}

func sectionHeader[E any](title string) core.Node[E] {
	title = strings.TrimSpace(title) // swobu:io-string source=boundary
	if title == "" {
		title = "section"
	}
	text := strings.Repeat(" ", 4) + title + " ▾"
	return core.Text[E](text).
		Style(core.Style{Token: core.TokenTextMuted, State: core.StateDefault})
}
