package routing

import (
	"os"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	appviews "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func firstRunProviderChoiceRow(label string, onActivate func() []update.Action) retained.ViewSpec[state.Model] {
	return toolkitviews.ListItemRow[state.Model](
		toolkitviews.InsetLabel(strings.TrimSpace(label), 4), // swobu:io-string source=boundary
		false,
		false,
		false,
		onActivate,
		nil,
	)
}

func firstRunRunOnSummary(provider string) string {
	if strings.TrimSpace(provider) == "" { // swobu:io-string source=boundary
		return appviews.ValueRequired
	}
	return providerDisplayName(provider)
}

func firstRunCredentialSummary(provider, baseURL, credentialRef string) string {
	if strings.TrimSpace(provider) == "" { // swobu:io-string source=boundary
		return appviews.ValueRequired
	}
	resolvedRef := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if resolvedRef == "" {
		if state.CreateDraftCredentialStrategySelectable(provider) {
			return appviews.ValueRequired
		}
		if !state.ProviderCredentialSelectionRequired(provider, baseURL, "") {
			return appviews.ValueAuto
		}
		return appviews.ValueRequired
	}
	if isResolvedInteractiveCredential(provider, resolvedRef) {
		return "signed in"
	}
	cred := credentialSource(resolvedRef)
	if cred != "" {
		if strings.EqualFold(provider, "bedrock") && isBedrockAWSProfileCredentialRef(resolvedRef) {
			return "external: AWS chain"
		}
		variant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(cred))) // swobu:io-string source=boundary
		if providercatalog.SupportsAuthVariant(provider, variant) {
			return authVariantDisplayLabel(variant)
		}
		if strings.EqualFold(cred, "env") {
			if strings.EqualFold(provider, "bedrock") {
				return "Bedrock API key"
			}
			key := strings.TrimSpace(envCredentialKey(resolvedRef)) // swobu:io-string source=boundary
			if key == "" {
				key = strings.TrimSpace(providercatalog.DefaultEnvKeyForSpec(provider)) // swobu:io-string source=boundary
			}
			if key != "" {
				if _, ok := os.LookupEnv(key); !ok {
					return appviews.ValueRequired
				}
			}
			return "env var"
		}
		if strings.EqualFold(cred, "file") {
			path := strings.TrimSpace(credentialFilePath(resolvedRef)) // swobu:io-string source=boundary
			if path == "" {
				return appviews.ValueRequired
			}
			if _, err := os.Stat(path); err != nil {
				return appviews.ValueRequired
			}
			return "file"
		}
		if strings.EqualFold(cred, "keychain") {
			return "signed in"
		}
		return cred
	}
	return appviews.ValueRequired
}

func isResolvedInteractiveCredential(provider, credentialRef string) bool {
	provider = strings.TrimSpace(provider)  // swobu:io-string source=boundary
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if provider == "" || ref == "" {
		return false
	}
	hasInteractive := false
	for _, variant := range providercatalog.SupportedAuthVariantsForSpec(provider) {
		if providercatalog.IsInteractiveAuthVariant(variant) {
			hasInteractive = true
			break
		}
	}
	if !hasInteractive {
		return false
	}
	source := strings.ToLower(strings.TrimSpace(credentialSource(ref))) // swobu:io-string source=boundary
	if source == "" {
		return false
	}
	if providercatalog.SupportsAuthVariant(provider, providercatalog.AuthVariant(source)) {
		return false
	}
	return !providercatalog.SupportsAuthVariant(provider, providercatalog.AuthVariant(source))
}

func createDraftCredentialRefFromActions(actions []update.Action) string {
	for _, action := range actions {
		if set, ok := action.(state.SetCreateDraftCredentialRef); ok {
			return strings.TrimSpace(set.CredentialRef) // swobu:io-string source=boundary
		}
	}
	return ""
}

func savedRoutingSummary(provider state.ProviderConfigSnapshot) string {
	spec := providerDisplayName(provider.ProviderSpec)
	cred := strings.TrimSpace(provider.CredentialRef) // swobu:io-string source=boundary
	if cred == "" {
		cred = defaultCreateDraftCredentialRef(provider.ProviderSpec)
	}
	modelID := providerHumanIdentifier(provider)
	if modelID == "" && cred == "" {
		return spec
	}
	if modelID == "" {
		return spec + " · " + cred
	}
	if cred == "" {
		return spec + " · " + modelID
	}
	return spec + " · " + cred + " · " + modelID
}

func workspaceRoutingSummary(provider state.ProviderConfigSnapshot) string {
	spec := providerDisplayName(provider.ProviderSpec)
	modelID := strings.TrimSpace(provider.ModelID) // swobu:io-string source=boundary
	if modelID == "" {
		return spec + " · models"
	}
	return spec + " · " + modelID + " · models"
}

func defaultCreateDraftCredentialRef(provider string) string {
	spec := strings.TrimSpace(strings.ToLower(provider)) // swobu:io-string source=boundary
	if spec == "" {
		return ""
	}
	if !providercatalog.RequiresCredential(spec, providercatalog.DefaultExecuteBaseURL(spec)) {
		return ""
	}
	return "env"
}

func effectiveDraftBaseURL(draft state.ProviderConfigSnapshot) string {
	provider := strings.TrimSpace(draft.ProviderSpec) // swobu:io-string source=boundary
	baseURL := strings.TrimSpace(draft.BaseURL)       // swobu:io-string source=boundary
	if baseURL != "" {
		return baseURL
	}
	if strings.EqualFold(provider, "bedrock") {
		if region := strings.TrimSpace(bedrockResolvedRegion(draft.Region, draft.BaseURL)); region != "" { // swobu:io-string source=boundary
			return strings.TrimSpace(bedrockBaseURLForRegion(region)) // swobu:io-string source=boundary
		}
	}
	return strings.TrimSpace(providercatalog.DefaultExecuteBaseURL(provider)) // swobu:io-string source=boundary
}

func effectiveCreateDraftBaseURL(model state.Model, provider string) string {
	draft := model.CreateDraftProviderConfig
	if strings.TrimSpace(draft.ProviderSpec) == "" { // swobu:io-string source=boundary
		draft.ProviderSpec = strings.TrimSpace(provider) // swobu:io-string source=boundary
	}
	return effectiveDraftBaseURL(draft)
}

func createSectionSummary(provider, modelID, credSummary string) string {
	summary := firstRunRunOnSummary(provider)
	if provider != "" {
		summary = providerDisplayName(provider) + " · " + selectors.EmptyOr(credSummary, appviews.ValueRequired)
		if modelID != "" {
			summary += " · " + modelID
		}
	}
	return summary
}
