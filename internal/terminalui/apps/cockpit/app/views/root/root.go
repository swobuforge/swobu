package root

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	appviews "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
)

// Root assembles the current Swobu cockpit page from app state, app-owned
// views, and generic toolkit batteries.
func Root() view.ViewSpec[state.Model] {
	return view.BuildWithLifecycle[state.Model](buildRoot, rootOnMountEffects, nil)
}

func buildRoot(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	bodyContent := view.Named[state.Model]("workspace/"+workspaceBodyKey(model), view.Build[state.Model](buildBodyCanvas))
	// Keep shell rails pinned: header/footer remain visible while only body
	// content scrolls.
	body := view.WithGrow[state.Model]()(view.Named[state.Model]("body", view.WithScrollY[state.Model](0)(bodyContent)))
	chrome := view.VStack(ctx,
		appviews.HeaderBar("Swobu! 🧌", selectors.HeaderShell(model)),
		appviews.HorizontalRule(),
		body,
		appviews.HorizontalRule(),
		appviews.FooterBar(selectors.FooterHints(model)),
	)
	guarded := toolkitviews.ViewportGuard(minViewportWidth, minViewportHeight, chrome)
	scoped := toolkitviews.FocusScope(guarded)
	tabScoped := toolkitviews.KeyScope(scoped, workspaceRailKeyHandler)
	return tabScoped
}

func workspaceBodyKey(model state.Model) string {
	if model.HelpTabOpen {
		return "__help__"
	}
	current := model.CurrentEndpoint
	if current == "" {
		return "__create__"
	}
	return current
}

func rootOnMountEffects() []update.Effect {
	return []update.Effect{
		stateeffect.RefreshDaemonStatusEffect{},
		stateeffect.RefreshEndpointsEffect{},
		stateeffect.RefreshCatalogEffect{},
		stateeffect.RefreshStatusProjectionEffect{},
		state.ScheduleDaemonRefreshEffect{},
	}
}

func workspaceRailKeyHandler(ctx *view.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
	if ev.Kind != interaction.EventKey {
		return false, nil
	}
	model := ctx.Model()
	if selectors.InteractionMode(model) != state.InteractionModeNAV {
		return false, nil
	}
	switch ev.Key {
	case interaction.KeyEsc:
		if model.HelpTabOpen {
			return true, []update.Action{state.SetHelpTabOpenAction{Open: false}}
		}
		return false, nil
	case interaction.KeyDown:
		return true, []update.Action{interaction.FocusMoveAction{Move: interaction.FocusMoveNext}}
	case interaction.KeyUp:
		return true, []update.Action{interaction.FocusMoveAction{Move: interaction.FocusMovePrev}}
	case interaction.KeyTab:
		help, endpoint := cycleTopTabSelection(model, false)
		return true, topTabActions(help, endpoint)
	case interaction.KeyShiftTab:
		help, endpoint := cycleTopTabSelection(model, true)
		return true, topTabActions(help, endpoint)
	case interaction.KeyRune:
		if ev.Rune == '?' {
			return true, []update.Action{state.SetHelpTabOpenAction{Open: true}}
		}
		return false, nil
	default:
		return false, nil
	}
}

func topTabActions(help bool, endpoint string) []update.Action {
	if help {
		return []update.Action{state.SetHelpTabOpenAction{Open: true}}
	}
	return []update.Action{
		state.SetHelpTabOpenAction{Open: false},
		state.SelectEndpoint{Name: endpoint},
	}
}

func cycleTopTabSelection(model state.Model, reverse bool) (bool, string) {
	items := []string{"__help__"}
	items = append(items, model.Endpoints...)
	items = append(items, "")
	if len(items) == 0 {
		return false, ""
	}
	index := 0
	if !model.HelpTabOpen {
		current := model.CurrentEndpoint
		index = len(items) - 1
		for i, item := range items {
			if item == "__help__" {
				continue
			}
			if strings.TrimSpace(item) == current {
				index = i
				break
			}
		}
	}
	for i, item := range items {
		if model.HelpTabOpen && item == "__help__" {
			index = i
			break
		}
	}
	if reverse {
		index = (index - 1 + len(items)) % len(items)
	} else {
		index = (index + 1) % len(items)
	}
	if items[index] == "__help__" {
		return true, ""
	}
	return false, strings.TrimSpace(items[index])
}
