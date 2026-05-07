// Body layout: section composition and responsive thresholds.
// The page owns cross-feature body layout as the screen-level coordinator.
package root

import (
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	appviews "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views/routing"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildBody returns the body layout for the current model state.
// Wide and narrow variants for responsive layout.
func BuildBody(ctx *retained.Context[state.Model], preset ScreenLayoutPreset) (retained.ViewSpec[state.Model], retained.ViewSpec[state.Model]) {
	model := ctx.Model()
	if model.HelpTabOpen {
		return helpBodyLayouts(ctx, preset)
	}
	if model.ControlPlane != nil {
		return incompatibilityBodyLayouts(ctx, preset)
	}
	if model.CurrentEndpoint == "" {
		return createBodyLayouts(ctx, model, preset)
	}
	return workspaceBodyLayouts(ctx, preset)
}

func helpBodyLayouts(ctx *retained.Context[state.Model], preset ScreenLayoutPreset) (retained.ViewSpec[state.Model], retained.ViewSpec[state.Model]) {
	body := composeSectionStack(ctx, preset.SectionGap, false,
		retained.Build[state.Model](appviews.BuildHelpSection),
	)
	return body, body
}

func incompatibilityBodyLayouts(ctx *retained.Context[state.Model], preset ScreenLayoutPreset) (retained.ViewSpec[state.Model], retained.ViewSpec[state.Model]) {
	body := composeSectionStack(ctx, preset.SectionGap, false,
		retained.Build[state.Model](appviews.BuildCompatibilityScreen),
	)
	return body, body
}

func createBodyLayouts(ctx *retained.Context[state.Model], _ state.Model, preset ScreenLayoutPreset) (retained.ViewSpec[state.Model], retained.ViewSpec[state.Model]) {
	model := ctx.Model()
	sections := []retained.ViewSpec[state.Model]{
		workspaceSection(),
		retained.Build[state.Model](routing.BuildSection),
		clientsSection(),
	}
	if model.InteractionMode != state.InteractionModePickOne {
		sections = append(sections, trafficSection())
	}
	addSpacer := shouldPadBeforeFooter(model)
	body := composeSectionStack(ctx, preset.SectionGap, addSpacer, sections...)
	return body, body
}

func workspaceBodyLayouts(ctx *retained.Context[state.Model], preset ScreenLayoutPreset) (retained.ViewSpec[state.Model], retained.ViewSpec[state.Model]) {
	body := composeSectionStack(ctx, preset.SectionGap, false,
		workspaceSection(),
		retained.Build[state.Model](routing.BuildSection),
		clientsSection(),
		trafficSection(),
	)
	return body, body
}

func composeSectionStack(ctx *retained.Context[state.Model], gapRows int, padBeforeFooter bool, sections ...retained.ViewSpec[state.Model]) retained.ViewSpec[state.Model] {
	stack := retained.VStackGap(ctx, max(0, gapRows), sections...)
	if !padBeforeFooter {
		return stack
	}
	return retained.VStack(ctx, stack, appviews.SummaryRow(""))
}

func shouldPadBeforeFooter(model state.Model) bool {
	if model.CurrentEndpoint != "" {
		return false
	}
	if model.InteractionMode == state.InteractionModePickOne {
		return false
	}
	provider := model.CreateDraftProviderConfig.ProviderSpec
	modelID := model.CreateDraftProviderConfig.ModelID
	if provider != "" && modelID == "" {
		return false
	}
	return true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func workspaceSection() retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](appviews.BuildWorkspaceSection)
}

func clientsSection() retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](appviews.BuildClientsSection)
}

func trafficSection() retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](appviews.BuildTrafficSection)
}
