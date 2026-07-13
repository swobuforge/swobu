package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildCockpitView is the SINGLE retained bridge for the entire cockpit.
// It is the ONLY place in the app that imports retained packages.
// All other views build core.Node values directly.
func BuildCockpitView(model state.Model) retained.ViewSpec[state.Model] {
	return retained.View[state.Model](func(ctx *retained.Context[state.Model]) retained.RenderNode {
		body := buildBodyNode(model)
		chrome := core.Stack[state.Action](core.AxisVertical,
			HeaderBarNode("⛉ SWOBU", headerShell(model)),
			headerRuleNode(),
			body,
			headerRuleNode(),
			FooterBarNode(footerHints(model)),
		)
		guard := viewportGuardNode(chrome)
		return lowerNode(guard)
	})
}

func buildBodyNode(model state.Model) core.Node[state.Action] {
	if model.HelpTabOpen {
		return BuildHelpSectionNode(model)
	}
	// TODO: other body sections as they migrate
	return core.Text[state.Action]("body")
}

func lowerNode(node core.Node[state.Action]) retained.RenderNode {
	renderNode, err := corelower.Lower(node, corelower.EnvConfig{DevMode: true}, func(a state.Action) update.Action {
		return a
	})
	if err != nil {
		return nil
	}
	return renderNode
}

// HeaderBarNode is the pure-core header bar.
func HeaderBarNode(left, right string) core.Node[state.Action] {
	return core.Text[state.Action](left + " " + right) // simplified; expand later
}

func headerRuleNode() core.Node[state.Action] {
	return core.Text[state.Action]("────────────────────────────────────────")
}

func FooterBarNode(hints string) core.Node[state.Action] {
	return core.Text[state.Action](hints)
}

func viewportGuardNode(child core.Node[state.Action]) core.Node[state.Action] {
	return child // placeholder for min viewport enforcement
}

func headerShell(model state.Model) string {
	return "ready" // simplified; route to selectors later
}

func footerHints(model state.Model) string {
	return "tab/shift+tab workspace   up/down move   enter act   esc back"
}
