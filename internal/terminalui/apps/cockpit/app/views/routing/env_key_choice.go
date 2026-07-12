package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
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
	normalizedProviderSpec := strings.TrimSpace(pc.ProviderSpec)         // swobu:io-string source=boundary
	normalizedProviderProtocol := strings.TrimSpace(pc.ProviderProtocol) // swobu:io-string source=boundary
	current := strings.TrimSpace(envCredentialKey(pc.CredentialRef))     // swobu:io-string source=boundary
	summary, editorValue := envKeySummary(normalizedProviderSpec, current)
	rowLabel := "env key"
	if strings.EqualFold(normalizedProviderSpec, "bedrock") {
		rowLabel = "env"
	}
	row := backendURLEditorRow(
		ctx,
		rowLabel,
		summary,
		editorValue,
		"env variable",
		func(value string) []update.Action {
			draftBaseURL := effectiveCreateDraftBaseURL(model, normalizedProviderSpec)
			return applyProviderEnvKeySelection(normalizedProviderSpec, normalizedProviderProtocol, value, spec.ProviderConfig, spec.EndpointName, spec.CreateMode, draftBaseURL)
		},
	)
	return row
}

func envKeySummary(providerSpec string, explicitKey string) (summary string, editorValue string) {
	if key := strings.TrimSpace(explicitKey); key != "" { // swobu:io-string source=boundary
		return key, key
	}
	if hint := strings.TrimSpace(profile.DefaultEnvKeyForSpec(providerSpec)); hint != "" { // swobu:io-string source=boundary
		return hint, hint
	}
	return "missing", ""
}

func applyProviderEnvKeySelection(providerSpec string, providerProtocol string, envKey string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool, createDraftBaseURL string) []update.Action {
	providerSpec = strings.TrimSpace(providerSpec)             // swobu:io-string source=boundary
	providerProtocol = strings.TrimSpace(providerProtocol)     // swobu:io-string source=boundary
	envKey = strings.TrimSpace(envKey)                         // swobu:io-string source=boundary
	endpointName = strings.TrimSpace(endpointName)             // swobu:io-string source=boundary
	createDraftBaseURL = strings.TrimSpace(createDraftBaseURL) // swobu:io-string source=boundary
	ref := encodeCredentialEnvRef(envKey)
	if createMode {
		baseURL := createDraftBaseURL
		if baseURL == "" {
			baseURL = strings.TrimSpace(profile.DefaultExecuteBaseURL(providerSpec))
		}
		baseURL = resolveBedrockMantleBaseURL(providerSpec, envKey, baseURL)
		authHeader := ""
		if providerConfig != nil {
			authHeader = strings.TrimSpace(providerConfig.AuthHeader) // swobu:io-string source=boundary
		}
		if strings.EqualFold(providerSpec, "openai_compatible") && authHeader == "" {
			authHeader = stateModel.ProviderDefaultAuthHeader(providerSpec)
		}
		return []update.Action{
			state.SetCreateDraftCredentialRef{CredentialRef: ref},
			state.SetCreateDraftModelIDAction{ModelID: ""},
			state.LoadRoutingModelCatalogRequestedAction{
				Scope:            state.RoutingModelCatalogScopeCreateDraft,
				ProviderSpec:     providerSpec,
				AuthHeader:       authHeader,
				ProviderProtocol: providerProtocol,
				BaseURL:          baseURL,
				CredentialRef:    ref,
			},
		}
	}
	if providerConfig == nil || endpointName == "" {
		return nil
	}
	next := *providerConfig
	next.CredentialRef = ref
	nextBaseURL := strings.TrimSpace(next.BaseURL) // swobu:io-string source=boundary
	next.BaseURL = resolveBedrockMantleBaseURL(providerSpec, envKey, nextBaseURL)
	return routingSaveProviderConfigActions(endpointName, next, "provider/env")
}

func resolveBedrockMantleBaseURL(providerSpec, envKey, currentBaseURL string) string {
	providerSpec = strings.TrimSpace(providerSpec)     // swobu:io-string source=boundary
	envKey = strings.TrimSpace(envKey)                 // swobu:io-string source=boundary
	currentBaseURL = strings.TrimSpace(currentBaseURL) // swobu:io-string source=boundary
	if currentBaseURL != "" {
		return currentBaseURL
	}
	if strings.EqualFold(providerSpec, "bedrock") {
		region := bedrockResolvedRegion("", currentBaseURL)
		if region == "" {
			region = bedrockDefaultRegionFromList()
		}
		return bedrockOpenAICompatibleBaseURLForRegion(region)
	}
	if strings.EqualFold(providerSpec, "openai_compatible") && strings.EqualFold(envKey, "AWS_BEARER_TOKEN_BEDROCK") {
		region := bedrockResolvedRegion("", currentBaseURL)
		if region == "" {
			region = bedrockDefaultRegionFromList()
		}
		return bedrockOpenAICompatibleBaseURLForRegion(region)
	}
	return currentBaseURL
}
