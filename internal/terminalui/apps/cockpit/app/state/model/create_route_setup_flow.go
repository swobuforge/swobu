package model

import (
	"net/url"
	"os"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

type RouteSetupSlotState string

const (
	RouteSetupSlotMissing  RouteSetupSlotState = "missing"
	RouteSetupSlotBlocked  RouteSetupSlotState = "blocked"
	RouteSetupSlotReady    RouteSetupSlotState = "ready"
	RouteSetupSlotExternal RouteSetupSlotState = "external"
	RouteSetupSlotLoading  RouteSetupSlotState = "loading"
	RouteSetupSlotFailed   RouteSetupSlotState = "failed"
)

type RouteSetupFlowState struct {
	ProviderState     RouteSetupSlotState
	Credential        RouteSetupSlotState
	CredentialVisible bool
	ScopeVisible      bool
	ScopeState        RouteSetupSlotState
	ModelState        RouteSetupSlotState
	ModelBlocker      string
	DeliveryState     RouteSetupSlotState
	Ready             bool
}

// EvaluateCreateDraftRouteSetup normalizes provider-specific draft values into
// one canonical setup progression state machine used by first-run/create flow.
func EvaluateCreateDraftRouteSetup(draft ProviderConfigSnapshot) RouteSetupFlowState {
	provider := strings.TrimSpace(draft.ProviderSpec) // swobu:io-string source=boundary
	baseURL := strings.TrimSpace(draft.BaseURL)       // swobu:io-string source=boundary
	region := strings.TrimSpace(draft.Region)         // swobu:io-string source=boundary
	effectiveBaseURL := baseURL
	if strings.EqualFold(provider, "bedrock") && effectiveBaseURL == "" {
		if region == "" {
			region = bedrockRegionFromEnv()
		}
		effectiveBaseURL = strings.TrimSpace(BedrockBaseURLForRegion(region)) // swobu:io-string source=boundary
	}
	credentialRef := strings.TrimSpace(draft.CredentialRef)
	modelID := strings.TrimSpace(draft.ModelID)

	out := RouteSetupFlowState{
		ProviderState:     RouteSetupSlotMissing,
		Credential:        RouteSetupSlotMissing,
		CredentialVisible: false,
		ScopeState:        RouteSetupSlotReady,
		ModelState:        RouteSetupSlotMissing,
		DeliveryState:     RouteSetupSlotReady,
	}
	if provider == "" {
		return out
	}
	out.ProviderState = RouteSetupSlotReady
	credentialRequired := ProviderCredentialSelectionRequired(provider, effectiveBaseURL, credentialRef)
	out.CredentialVisible = CreateDraftCredentialStrategySelectable(provider) || credentialRequired
	out.Credential = evaluateCreateDraftCredentialState(provider, effectiveBaseURL, credentialRef, out.CredentialVisible)

	out.ScopeVisible = providerRequiresScopeSlot(provider)
	out.ScopeState = evaluateScopeState(provider, effectiveBaseURL, out.ScopeVisible)
	out.ModelState, out.ModelBlocker = evaluateModelState(provider, modelID, out.Credential, out.ScopeVisible, out.ScopeState)

	out.Ready = out.ProviderState == RouteSetupSlotReady &&
		(out.Credential == RouteSetupSlotReady || out.Credential == RouteSetupSlotExternal) &&
		(!out.ScopeVisible || out.ScopeState == RouteSetupSlotReady) &&
		out.ModelState == RouteSetupSlotReady
	return out
}

func bedrockRegionFromEnv() string {
	if region := strings.TrimSpace(os.Getenv("AWS_REGION")); region != "" { // swobu:io-string source=boundary
		return region
	}
	return strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION")) // swobu:io-string source=boundary
}

func evaluateCreateDraftCredentialState(provider, baseURL, credentialRef string, credentialVisible bool) RouteSetupSlotState {
	if !ProviderCredentialSelectionRequired(provider, baseURL, credentialRef) {
		if credentialRef == "" && credentialVisible {
			return RouteSetupSlotMissing
		}
		return RouteSetupSlotExternal
	}
	if credentialRef == "" {
		return RouteSetupSlotMissing
	}
	if ProviderCredentialVariantIsInteractive(provider, credentialRef) {
		return RouteSetupSlotLoading
	}
	if isExternalCredentialAuthorityVariant(provider, credentialRef) {
		return RouteSetupSlotExternal
	}
	source := credentialSource(credentialRef)
	if strings.EqualFold(source, "file") && strings.TrimSpace(fileCredentialPath(credentialRef)) == "" { // swobu:io-string source=boundary
		return RouteSetupSlotMissing
	}
	return RouteSetupSlotReady
}

func evaluateScopeState(provider, baseURL string, visible bool) RouteSetupSlotState {
	if !visible {
		return RouteSetupSlotReady
	}
	if createDraftScopeMissing(provider, baseURL) {
		return RouteSetupSlotMissing
	}
	return RouteSetupSlotReady
}

func evaluateModelState(provider, modelID string, credentialState RouteSetupSlotState, scopeVisible bool, scopeState RouteSetupSlotState) (RouteSetupSlotState, string) {
	if credentialState == RouteSetupSlotMissing || credentialState == RouteSetupSlotLoading {
		return RouteSetupSlotBlocked, "set credential before loading models"
	}
	if scopeVisible && scopeState == RouteSetupSlotMissing {
		if strings.EqualFold(provider, "bedrock") {
			return RouteSetupSlotBlocked, "choose region before loading models"
		}
		return RouteSetupSlotBlocked, "choose scope before loading models"
	}
	if modelID == "" {
		return RouteSetupSlotMissing, ""
	}
	return RouteSetupSlotReady, ""
}

func providerRequiresScopeSlot(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "bedrock") || // swobu:io-string source=boundary
		strings.EqualFold(strings.TrimSpace(provider), "openai_compatible")
}

func CreateDraftCredentialStrategySelectable(provider string) bool {
	provider = strings.TrimSpace(provider) // swobu:io-string source=boundary
	if provider == "" {
		return false
	}
	return len(providercatalog.SupportedAuthVariantsForSpec(provider)) > 0
}

func isExternalCredentialAuthorityVariant(provider, credentialRef string) bool {
	source := strings.TrimSpace(credentialSource(credentialRef)) // swobu:io-string source=boundary
	if source == "" {
		return false
	}
	variant := providercatalog.AuthVariant(strings.ToLower(source))                             // swobu:io-string source=boundary
	for _, mode := range providercatalog.AllowedAuthModesForSpec(strings.TrimSpace(provider)) { // swobu:io-string source=boundary
		if mode.Variant == variant && mode.Kind == providercatalog.AuthNone {
			return true
		}
	}
	return false
}

func createDraftScopeMissing(provider, baseURL string) bool {
	provider = strings.TrimSpace(provider) // swobu:io-string source=boundary
	baseURL = strings.TrimSpace(baseURL)   // swobu:io-string source=boundary
	if strings.EqualFold(provider, "openai_compatible") {
		return baseURL == ""
	}
	if !strings.EqualFold(provider, "bedrock") {
		return false
	}
	if baseURL == "" {
		return true
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return true
	}
	host := strings.TrimSpace(strings.ToLower(u.Hostname())) // swobu:io-string source=boundary
	parts := strings.Split(host, ".")
	if len(parts) < 4 {
		return true
	}
	if !strings.HasPrefix(parts[0], "bedrock-runtime") {
		return true
	}
	return strings.TrimSpace(parts[1]) == ""
}
