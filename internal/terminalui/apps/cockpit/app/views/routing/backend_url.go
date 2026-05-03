// Provider backend URL row.
package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
)

type providerBackendURLRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerBackendURLRow(spec providerBackendURLRowSpec) view.ViewSpec[state.Model] {
	return view.Build[state.Model](func(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
		return buildProviderBackendURLRow(ctx, spec)
	})
}

func buildProviderBackendURLRow(ctx *view.Context[state.Model], spec providerBackendURLRowSpec) view.ViewSpec[state.Model] {
	model := ctx.Model()
	pc := selectedProvider(model, spec.ProviderConfig, spec.CreateMode)
	var out view.ViewSpec[state.Model]
	if pc == nil || strings.TrimSpace(pc.ProviderSpec) != "custom" {
		return nil
	}
	parent := backendURLEditorRow(ctx, views.RowBackendURL, selectors.EmptyOr(strings.TrimSpace(pc.BaseURL), "missing"), strings.TrimSpace(pc.BaseURL), "https://host/v1", func(value string) []update.Action {
		return applyProviderBackendURL(value, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
	})
	if strings.TrimSpace(pc.BaseURL) == "" {
		out = toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows("custom backend URL is required (https://host/v1)")...)
	} else if model.RoutingSaveError != "" {
		out = toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows(model.RoutingSaveError)...)
	} else {
		out = parent
	}
	return out
}

func backendURLEditorRow(ctx *view.Context[state.Model], label, summary, currentValue, emptyStateLabel string, save func(string) []update.Action) view.ViewSpec[state.Model] {
	open, setOpen := view.UseState(ctx, func() bool { return false })
	draft, setDraft := view.UseState(ctx, func() string { return currentValue })
	parent := views.RowEditWithCancel(label, summary, func() []update.Action {
		setDraft(currentValue)
		nextOpen := !open
		setOpen(nextOpen)
		mode := state.InteractionModeManageList
		if nextOpen {
			mode = state.InteractionModeEditText
		}
		return []update.Action{state.SetInteractionMode{Mode: mode}}
	}, func() []update.Action {
		if open {
			setOpen(false)
			setDraft(currentValue)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeManageList}}
		}
		return nil
	})
	if !open {
		return parent
	}
	return toolkitviews.NewAnchoredDisclosure(parent, view.Named[state.Model]("editor", views.InlineEditor(
		label,
		draft,
		emptyStateLabel,
		func(value string) []update.Action {
			setDraft(value)
			return nil
		},
		func(value string) []update.Action {
			setOpen(false)
			actions := save(strings.TrimSpace(value))
			return append([]update.Action{state.SetInteractionMode{Mode: state.InteractionModeManageList}}, actions...)
		},
		func() []update.Action {
			setOpen(false)
			setDraft(currentValue)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeManageList}}
		},
	)))
}

func applyProviderBackendURL(baseURL string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	baseURL = strings.TrimSpace(baseURL)
	if createMode {
		return []update.Action{state.SetCreateDraftBaseURL{BaseURL: baseURL}}
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" {
		return nil
	}
	next := *providerConfig
	next.BaseURL = baseURL
	return []update.Action{
		state.RoutingSaveStartedAction{},
		state.SaveProviderConfigRequested{
			EndpointName:   strings.TrimSpace(endpointName),
			ProviderConfig: next,
		},
	}
}
