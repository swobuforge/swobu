package routing

import (
	"strings"

	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/view"
)

type providerTargetAliasRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerTargetAliasRow(spec providerTargetAliasRowSpec) view.ViewSpec[state.Model] {
	return view.Build[state.Model](func(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
		return buildProviderTargetAliasRow(ctx, spec)
	})
}

func buildProviderTargetAliasRow(ctx *view.Context[state.Model], spec providerTargetAliasRowSpec) view.ViewSpec[state.Model] {
	pc := selectedProvider(ctx.Model(), spec.ProviderConfig, spec.CreateMode)
	if pc == nil {
		return nil
	}
	currentValue := strings.TrimSpace(pc.TargetAlias)
	summary := "not set"
	if currentValue != "" {
		summary = currentValue
	}
	return backendURLEditorRow(ctx, views.RowTargetAlias, summary, currentValue, "fast", func(value string) []update.Action {
		return applyProviderTargetAlias(value, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
	})
}

func applyProviderTargetAlias(targetAlias string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	targetAlias = strings.ToLower(strings.TrimSpace(targetAlias))
	if createMode {
		return []update.Action{state.SetCreateDraftTargetAlias{TargetAlias: targetAlias}}
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" {
		return nil
	}
	next := *providerConfig
	next.TargetAlias = targetAlias
	return []update.Action{
		state.RoutingSaveStartedAction{},
		state.SaveProviderConfigRequested{
			EndpointName:   strings.TrimSpace(endpointName),
			ProviderConfig: next,
		},
	}
}
