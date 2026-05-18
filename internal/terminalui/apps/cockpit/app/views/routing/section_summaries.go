package routing

import (
	"os"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
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
		return "choose a provider"
	}
	return providerDisplayName(provider)
}

func firstRunCredentialSummary(provider, baseURL, credentialRef string) string {
	if strings.TrimSpace(provider) == "" { // swobu:io-string source=boundary
		return "missing"
	}
	resolvedRef := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if resolvedRef == "" {
		if state.CreateDraftCredentialStrategySelectable(provider) {
			return "missing"
		}
		if !state.ProviderCredentialSelectionRequired(provider, baseURL, "") {
			return "external"
		}
		return "missing"
	}
	if isResolvedInteractiveCredential(provider, resolvedRef) {
		return "signed in"
	}
	cred := credentialSource(resolvedRef)
	if cred != "" {
		if strings.EqualFold(provider, "bedrock") && isBedrockAWSProfileCredentialRef(resolvedRef) {
			return "external: AWS profile"
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
					return "env var missing"
				}
			}
			return "env var"
		}
		if strings.EqualFold(cred, "file") {
			path := strings.TrimSpace(credentialFilePath(resolvedRef)) // swobu:io-string source=boundary
			if path == "" {
				return "file missing"
			}
			if _, err := os.Stat(path); err != nil {
				return "file missing"
			}
			return "file"
		}
		if strings.EqualFold(cred, "keychain") {
			return "signed in"
		}
		return cred
	}
	return "missing"
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

func effectiveCreateDraftBaseURL(model state.Model, provider string) string {
	baseURL := model.CreateDraftProviderConfig.BaseURL
	if baseURL != "" {
		return baseURL
	}
	return strings.TrimSpace(providercatalog.DefaultExecuteBaseURL(provider)) // swobu:io-string source=boundary
}

func createSectionSummary(provider, modelID, credSummary string) string {
	summary := firstRunRunOnSummary(provider)
	if provider != "" {
		summary = providerDisplayName(provider) + " · " + selectors.EmptyOr(credSummary, "not set")
		if modelID != "" {
			summary += " · " + modelID
		}
	}
	return summary
}
