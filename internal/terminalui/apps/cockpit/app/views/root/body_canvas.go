// Body layout: section composition and responsive thresholds.
// The page owns cross-feature body layout as the screen-level coordinator.
package root

import (
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	appviews "github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/views/routing"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/view"
)

// BuildBody returns the body layout for the current model state.
// Wide and narrow variants for responsive layout.
func BuildBody(ctx *view.Context[state.Model], preset ScreenLayoutPreset) (view.ViewSpec[state.Model], view.ViewSpec[state.Model]) {
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

func helpBodyLayouts(ctx *view.Context[state.Model], preset ScreenLayoutPreset) (view.ViewSpec[state.Model], view.ViewSpec[state.Model]) {
	body := composeSectionStack(ctx, preset.SectionGap, false,
		view.Build[state.Model](appviews.BuildHelpSection),
	)
	return body, body
}

func incompatibilityBodyLayouts(ctx *view.Context[state.Model], preset ScreenLayoutPreset) (view.ViewSpec[state.Model], view.ViewSpec[state.Model]) {
	body := composeSectionStack(ctx, preset.SectionGap, false,
		view.Build[state.Model](appviews.BuildCompatibilityScreen),
	)
	return body, body
}

func createBodyLayouts(ctx *view.Context[state.Model], _ state.Model, preset ScreenLayoutPreset) (view.ViewSpec[state.Model], view.ViewSpec[state.Model]) {
	model := ctx.Model()
	sections := []view.ViewSpec[state.Model]{
		workspaceSection(),
		view.Build[state.Model](routing.BuildSection),
		clientsSection(),
	}
	if model.InteractionMode != state.InteractionModePickOne {
		sections = append(sections, trafficSection())
	}
	addSpacer := shouldPadBeforeFooter(model)
	body := composeSectionStack(ctx, preset.SectionGap, addSpacer, sections...)
	return body, body
}

func workspaceBodyLayouts(ctx *view.Context[state.Model], preset ScreenLayoutPreset) (view.ViewSpec[state.Model], view.ViewSpec[state.Model]) {
	body := composeSectionStack(ctx, preset.SectionGap, false,
		workspaceSection(),
		view.Build[state.Model](routing.BuildSection),
		clientsSection(),
		trafficSection(),
	)
	return body, body
}

func composeSectionStack(ctx *view.Context[state.Model], gapRows int, padBeforeFooter bool, sections ...view.ViewSpec[state.Model]) view.ViewSpec[state.Model] {
	stack := view.VStackGap(ctx, max(0, gapRows), sections...)
	if !padBeforeFooter {
		return stack
	}
	return view.VStack(ctx, stack, appviews.SummaryRow(""))
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

func workspaceSection() view.ViewSpec[state.Model] {
	return view.Build[state.Model](appviews.BuildWorkspaceSection)
}

func clientsSection() view.ViewSpec[state.Model] {
	return view.Build[state.Model](appviews.BuildClientsSection)
}

func trafficSection() view.ViewSpec[state.Model] {
	return view.Build[state.Model](appviews.BuildTrafficSection)
}
