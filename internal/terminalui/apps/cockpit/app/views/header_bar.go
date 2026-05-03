// Cockpit shell views: header bar, workspace rail, footer.
package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
)

// HeaderBar renders the shell title/status row.
func HeaderBar(left, right string) view.ViewSpec[state.Model] {
	return view.FromRenderNode[state.Model](toolkitviews.NewAction(headerIntrinsicWidth(left, right), false, false, func(_ bool, width int) string {
		return renderHeaderLine(width, left, right)
	}, nil, nil))
}

func headerIntrinsicWidth(left, right string) int {
	return len([]rune(strings.TrimSpace(left))) + len([]rune(strings.TrimSpace(right))) + 1
}

func renderHeaderLine(width int, left, right string) string {
	return toolkitviews.RenderSplitLine(width, left, right)
}

// WorkspaceRail renders the endpoint tab strip.
func WorkspaceRail(endpoints []string, current string) view.ViewSpec[state.Model] {
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
	return view.FromRenderNode[state.Model](toolkitviews.NewChoiceListWithFocusable(items, selected, toolkitviews.ChoiceListAxisHorizontal, false, func(label string, selected bool) string {
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
func FooterBar(hints string) view.ViewSpec[state.Model] {
	line := strings.TrimSpace(hints)
	if line == "" {
		line = "tab/shift+tab workspace   up/down move   enter act   esc back"
	}
	return StaticTextLine[state.Model](line)
}

// --- SectionHeader ---

func NewSectionHeader[M any](title string) view.ViewSpec[M] {
	text := strings.Repeat(" ", max(0, InsetSection)) + strings.TrimSpace(title) + " ▾"
	return view.FromRenderNode[M](toolkitviews.NewAction(toolkitviews.RuneLen(text), false, false, func(_ bool, width int) string {
		return toolkitviews.PadRight(toolkitviews.TrimToWidth(text, width), width)
	}, nil, nil))
}

// HorizontalRule renders one full-width separator line.
func HorizontalRule() view.ViewSpec[state.Model] {
	return view.FromRenderNode[state.Model](toolkitviews.NewAction(1, false, false, func(_ bool, width int) string {
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
