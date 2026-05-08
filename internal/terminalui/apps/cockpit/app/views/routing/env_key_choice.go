package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// providerEnvKeyRowSpec owns env-key selection when env-backed credentials are selected.
type providerEnvKeyRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerEnvKeyRow(spec providerEnvKeyRowSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildProviderEnvKeyRow(ctx, spec)
	})
}

func buildProviderEnvKeyRow(ctx *retained.Context[state.Model], spec providerEnvKeyRowSpec) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	pc := selectedProvider(model, spec.ProviderConfig, spec.CreateMode)
	if pc == nil || !strings.EqualFold(credentialSource(pc.CredentialRef), "env") {
		return nil
	}
	current := strings.TrimSpace(envCredentialKey(pc.CredentialRef))
	summary, editorValue := envKeySummary(strings.TrimSpace(pc.ProviderSpec), current)
	row := backendURLEditorRow(
		ctx,
		"env key",
		summary,
		editorValue,
		"env variable",
		func(value string) []update.Action {
			draftBaseURL := model.CreateDraftProviderConfig.BaseURL
			return applyProviderEnvKeySelection(strings.TrimSpace(pc.ProviderSpec), value, spec.ProviderConfig, spec.EndpointName, spec.CreateMode, draftBaseURL)
		},
	)
	return row
}

func envKeySummary(providerSpec string, explicitKey string) (summary string, editorValue string) {
	if key := strings.TrimSpace(explicitKey); key != "" {
		return key, key
	}
	if hint := strings.TrimSpace(providercatalog.DefaultEnvKeyForSpec(providerSpec)); hint != "" {
		return hint, hint
	}
	return "missing", ""
}

func applyProviderEnvKeySelection(providerSpec string, envKey string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool, createDraftBaseURL string) []update.Action {
	ref := encodeCredentialEnvRef(envKey)
	if createMode {
		baseURL := strings.TrimSpace(createDraftBaseURL)
		if baseURL == "" {
			baseURL = strings.TrimSpace(providercatalog.DefaultBaseURL(providerSpec))
		}
		return []update.Action{
			state.SetCreateDraftCredentialRef{CredentialRef: ref},
			state.SetCreateDraftModelID{ModelID: ""},
			state.LoadRoutingModelCatalogRequested{
				Scope:         state.RoutingModelCatalogScopeCreateDraft,
				ProviderSpec:  strings.TrimSpace(providerSpec),
				BaseURL:       baseURL,
				CredentialRef: ref,
				ProtocolKind:  defaultProtocolKindForProvider(providerSpec),
			},
		}
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" {
		return nil
	}
	next := *providerConfig
	next.CredentialRef = ref
	return []update.Action{
		state.RoutingSaveStartedAction{},
		state.SaveProviderConfigRequested{
			EndpointName:   strings.TrimSpace(endpointName),
			ProviderConfig: next,
		},
	}
}
