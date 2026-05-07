package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type providerTargetAliasRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerTargetAliasRow(spec providerTargetAliasRowSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildProviderTargetAliasRow(ctx, spec)
	})
}

func buildProviderTargetAliasRow(ctx *retained.Context[state.Model], spec providerTargetAliasRowSpec) retained.ViewSpec[state.Model] {
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
