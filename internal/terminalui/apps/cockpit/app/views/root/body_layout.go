// Body layout: viewport mechanics and responsive thresholds.
package root

import (
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	appviews "github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/view"
)

const (
	minViewportWidth       = 60
	minViewportHeight      = 18
	shellHorizontalPadding = 2
	splitViewportWidth     = 132
)

type ScreenLayoutPreset struct {
	SectionGap      int
	ContentMaxWidth int
}

var OnboardingLayoutPreset = ScreenLayoutPreset{
	SectionGap:      appviews.StackGap,
	ContentMaxWidth: appviews.ContentMaxWidth,
}

var WorkspaceLayoutPreset = ScreenLayoutPreset{
	SectionGap:      appviews.StackGap,
	ContentMaxWidth: 0,
}

// buildBodyCanvas renders the cockpit body area with responsive layout.
func buildBodyCanvas(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	preset := activeLayoutPreset(model)
	wide, narrow := BuildBody(ctx, preset)
	if preset.ContentMaxWidth > 0 {
		wide = view.WithMaxWidth[state.Model](preset.ContentMaxWidth)(wide)
		narrow = view.WithMaxWidth[state.Model](preset.ContentMaxWidth)(narrow)
	}
	return view.ResponsiveView[state.Model]{
		Threshold: splitViewportWidth - shellHorizontalPadding,
		Wide:      wide,
		Narrow:    narrow,
	}
}

func activeLayoutPreset(model state.Model) ScreenLayoutPreset {
	if model.ControlPlane != nil {
		return WorkspaceLayoutPreset
	}
	if model.CurrentEndpoint == "" {
		return OnboardingLayoutPreset
	}
	if model.HeaderStatus == "saved" && len(model.Endpoints) == 1 {
		return OnboardingLayoutPreset
	}
	return WorkspaceLayoutPreset
}
