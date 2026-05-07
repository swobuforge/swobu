// Cockpit shell views: header bar, workspace rail, footer.
package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// HeaderBar renders the shell title/status row.
func HeaderBar(left, right string) retained.ViewSpec[state.Model] {
	return retained.FromRenderNode[state.Model](toolkitviews.NewAction(headerIntrinsicWidth(left, right), false, false, func(_ bool, width int) string {
		return renderHeaderLine(width, left, right)
	}, nil, nil))
}

func headerIntrinsicWidth(left, right string) int {
	return toolkitviews.RuneLen(strings.TrimSpace(left)) + toolkitviews.RuneLen(strings.TrimSpace(right)) + 1
}

func renderHeaderLine(width int, left, right string) string {
	return toolkitviews.RenderSplitLine(width, left, right)
}

// WorkspaceRail renders the endpoint tab strip.
func WorkspaceRail(endpoints []string, current string) retained.ViewSpec[state.Model] {
	items := append([]string(nil), endpoints...)
	items = append(items, "+")
	selected := len(items) - 1
	current = strings.TrimSpace(current)
	for i, name := range endpoints {
		if strings.TrimSpace(name) == current && current != "" {
			selected = i
			break
		}
	}
	endpointNames := endpoints
	return retained.FromRenderNode[state.Model](toolkitviews.NewChoiceListWithFocusable(items, selected, toolkitviews.ChoiceListAxisHorizontal, false, func(label string, selected bool) string {
		label = strings.TrimSpace(label)
		if selected {
			return "[› " + label + "]"
		}
		return "[ " + label + " ]"
	}, func(index int) []update.Action {
		name := ""
		if index >= 0 && index < len(endpointNames) {
			name = strings.TrimSpace(endpointNames[index])
		}
		return []update.Action{state.SelectEndpoint{Name: name}}
	}))
}

// FooterBar renders the operator hint line.
func FooterBar(hints string) retained.ViewSpec[state.Model] {
	line := strings.TrimSpace(hints)
	if line == "" {
		line = "tab/shift+tab workspace   up/down move   enter act   esc back"
	}
	return StaticTextLine[state.Model](line)
}

// --- SectionHeader ---

func NewSectionHeader[M any](title string) retained.ViewSpec[M] {
	text := strings.Repeat(" ", max(0, InsetSection)) + strings.TrimSpace(title) + " ▾"
	return retained.FromRenderNode[M](toolkitviews.NewAction(toolkitviews.RuneLen(text), false, false, func(_ bool, width int) string {
		return toolkitviews.PadRight(toolkitviews.TrimToWidth(text, width), width)
	}, nil, nil))
}

// HorizontalRule renders one full-width separator line.
func HorizontalRule() retained.ViewSpec[state.Model] {
	return retained.FromRenderNode[state.Model](toolkitviews.NewAction(1, false, false, func(_ bool, width int) string {
		if width <= 0 {
			return ""
		}
		return strings.Repeat("─", width)
	}, nil, nil))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
