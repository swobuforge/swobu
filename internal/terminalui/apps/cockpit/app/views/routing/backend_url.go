// Provider backend URL row.
package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type providerBackendURLRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerBackendURLRow(spec providerBackendURLRowSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildProviderBackendURLRow(ctx, spec)
	})
}

func buildProviderBackendURLRow(ctx *retained.Context[state.Model], spec providerBackendURLRowSpec) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	pc := selectedProvider(model, spec.ProviderConfig, spec.CreateMode)
	var out retained.ViewSpec[state.Model]
	if pc == nil || strings.TrimSpace(pc.ProviderSpec) != "openai_compatible" { // swobu:io-string source=boundary
		return nil
	}
	parent := backendURLEditorRow(ctx, views.RowBackendURL, selectors.EmptyOr(strings.TrimSpace(pc.BaseURL), "missing"), strings.TrimSpace(pc.BaseURL), "https://host/v1", func(value string) []update.Action { // swobu:io-string source=boundary
		return applyProviderBackendURL(value, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
	})
	if strings.TrimSpace(pc.BaseURL) == "" { // swobu:io-string source=boundary
		out = toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows("OpenAI-compatible backend URL is required (https://host/v1)")...)
	} else if message := views.ScopedError(model, "routing", "provider/backend-url"); message != "" {
		out = toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows(message)...)
	} else {
		out = parent
	}
	return out
}

func backendURLEditorRow(ctx *retained.Context[state.Model], label, summary, currentValue, emptyStateLabel string, save func(string) []update.Action) retained.ViewSpec[state.Model] {
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	draft, setDraft := retained.UseState(ctx, func() string { return currentValue })
	if open {
		return retained.Named[state.Model]("inline-editor", views.InlineEditor(
			label,
			draft,
			emptyStateLabel,
			func(value string) []update.Action {
				setDraft(value)
				return nil
			},
			func(value string) []update.Action {
				setOpen(false)
				actions := save(strings.TrimSpace(value)) // swobu:io-string source=boundary
				return append([]update.Action{state.SetInteractionMode{Mode: state.InteractionModeManageList}}, actions...)
			},
			func() []update.Action {
				setOpen(false)
				setDraft(currentValue)
				return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeManageList}}
			},
		))
	}
	return views.RowEditWithCancel(label, summary, func() []update.Action {
		setDraft(currentValue)
		setOpen(true)
		return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeEditText}}
	}, nil)
}

func applyProviderBackendURL(baseURL string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	baseURL = strings.TrimSpace(baseURL) // swobu:io-string source=boundary
	if createMode {
		return []update.Action{state.SetCreateDraftBaseURL{BaseURL: baseURL}}
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" { // swobu:io-string source=boundary
		return nil
	}
	next := *providerConfig
	next.BaseURL = baseURL
	return routingSaveProviderConfigActions(strings.TrimSpace(endpointName), next, "provider/backend-url") // swobu:io-string source=boundary
}
