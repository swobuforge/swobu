// Cockpit shell views: header bar, workspace rail, footer.
package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// HeaderBarNode returns a core.Node for the shell title/status row.
func HeaderBarNode(left, right string) core.Node[state.Action] {
	return core.Text[state.Action](renderHeaderLine(headerIntrinsicWidth(left, right), left, right)).
		Style(core.Style{Token: core.TokenTextDefault, State: core.StateDefault}).
		Layout(core.Layout{
			Size: core.Size{Width: core.Fit(), Height: core.Fit()},
		})
}

// FooterBarNode returns a core.Node for the operator hint line.
func FooterBarNode(hints string) core.Node[state.Action] {
	line := strings.TrimSpace(hints) // swobu:io-string source=boundary
	if line == "" {
		line = "tab/shift+tab workspace   up/down move   enter act   esc back"
	}
	return StaticTextLineNode(line)
}

// WorkspaceRailNode returns a core.Node for the endpoint tab strip.
func WorkspaceRailNode(endpoints []string, current string) core.Node[state.Action] {
	items := append([]string(nil), endpoints...)
	items = append(items, "+")
	selected := len(items) - 1
	if current != "" {
		for i, name := range endpoints {
			if strings.TrimSpace(name) == current { // swobu:io-string source=boundary
				selected = i
				break
			}
		}
	}
	var children []core.Node[state.Action]
	for i, label := range items {
		label = strings.TrimSpace(label) // swobu:io-string source=boundary
		isSelected := i == selected
		var rendered string
		if isSelected {
			rendered = "[› " + label + "]"
		} else {
			rendered = "[ " + label + " ]"
		}
		var choiceName string
		if i < len(endpoints) {
			choiceName = endpoints[i]
		}
		child := core.Action(rendered, core.SignalEvent[state.Action]{
			Kind:  "workspace.select",
			Event: state.SelectEndpoint{Name: strings.TrimSpace(choiceName)},
		}).Key(core.Key("rail/" + choiceName))
		if !isSelected {
			child = child.Style(core.Style{Token: core.TokenTextDefault, State: core.StateDefault})
		}
		children = append(children, child)
	}
	return core.Stack[state.Action](core.AxisHorizontal, children...).
		Layout(core.Layout{
			Size:  core.Size{Width: core.Fill(1), Height: core.Fit()},
			Flow:  core.Flow{Mode: core.FlowStack, Axis: core.AxisHorizontal},
			Inset: core.Insets{Left: shellHorizontalPadding},
		})
}

// SectionHeaderNode returns a core.Node for a section header label.
func SectionHeaderNode(title string) core.Node[state.Action] {
	text := strings.Repeat(" ", max(0, InsetSection)) + strings.TrimSpace(title) + " ▾" // swobu:io-string source=boundary
	return core.Text[state.Action](text).
		Style(core.Style{Token: core.TokenTextMuted, State: core.StateDefault})
}

// retained wrappers (deprecated — use *Node variants)

// HeaderBar renders the shell title/status row.
func HeaderBar(left, right string) retained.ViewSpec[state.Model] {
	return CoreNodeAsRetained(HeaderBarNode(left, right))
}

// FooterBar renders the operator hint line.
func FooterBar(hints string) retained.ViewSpec[state.Model] {
	return CoreNodeAsRetained(FooterBarNode(hints))
}

// WorkspaceRail renders the endpoint tab strip.
func WorkspaceRail(endpoints []string, current string) retained.ViewSpec[state.Model] {
	return CoreNodeAsRetained(WorkspaceRailNode(endpoints, current))
}

// HorizontalRule renders one full-width separator line.
func HorizontalRule() retained.ViewSpec[state.Model] {
	return CoreNodeAsRetained(HorizontalRuleNode())
}

// SectionHeader renders one section header label.
func NewSectionHeader(title string) retained.ViewSpec[state.Model] {
	return CoreNodeAsRetained(SectionHeaderNode(title))
}

// HorizontalRuleNode returns one full-width separator line as a core.Node.
func HorizontalRuleNode() core.Node[state.Action] {
	return core.Text[state.Action]("────────────────────────────────────────────────────────────────────────────").
		Style(core.Style{Token: core.TokenTextMuted, State: core.StateDefault})
}

func headerIntrinsicWidth(left, right string) int {
	return toolkitviews.RuneLen(left) + toolkitviews.RuneLen(right) + 1 // swobu:io-string source=boundary
}

func renderHeaderLine(width int, left, right string) string {
	if width <= 0 {
		return ""
	}
	left = strings.TrimSpace(left)   // swobu:io-string source=boundary
	right = strings.TrimSpace(right) // swobu:io-string source=boundary
	midW := rawMax(0, width-len(left)-len(right))
	return left + strings.Repeat(" ", midW) + right
}

func rawMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

const (
	renderHeaderLineWidth  = 80
	shellHorizontalPadding = 2
)
