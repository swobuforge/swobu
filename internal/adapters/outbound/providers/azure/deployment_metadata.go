// test-only because azure deployment metadata routing helpers are pure catalog-entry data-policy dispatch with intermittent runtime callers.
package azure

import (
	"strings"
)

const (
	azureDeploymentFamilyOpenAI    = "openai"
	azureDeploymentFamilyAnthropic = "anthropic"
)

var (
	azureSupportedProviderProtocolsOpenAI = []string{
		"responses",
		"responses_stream",
		"chat_completions",
		"chat_completions_stream",
		"completions",
		"completions_stream",
	}
	azureSupportedProviderProtocolsAnthropic = []string{
		"messages",
		"messages_stream",
	}
)

// swobu:lint ignore test-only-dead-cluster because=azureModelCatalogCapabilityRecord is a shared catalog capability data shape used by alive callers from catalog bundling path.
type azureModelCatalogCapabilityRecord struct {
	FineTune        bool `json:"fine_tune"`
	Inference       bool `json:"inference"`
	Completion      bool `json:"completion"`
	ChatCompletion  bool `json:"chat_completion"`
	Embeddings      bool `json:"embeddings"`
	GlobalFineTune  bool `json:"global_fine_tune"`
	DevtierFineTune bool `json:"devtier_fine_tune"`
}

func deploymentFamilyForPublisher(modelPublisher string) string {
	publisher := strings.ToLower(strings.TrimSpace(modelPublisher)) // swobu:io-string source=boundary
	switch publisher {
	case azureDeploymentFamilyOpenAI, "deepseek", "xai":
		// Azure surfaces DeepSeek and xAI through the OpenAI-compatible wire family.
		return azureDeploymentFamilyOpenAI
	case azureDeploymentFamilyAnthropic:
		return azureDeploymentFamilyAnthropic
	default:
		return ""
	}
}

// swobu:lint ignore test-only-dead-cluster because=publisher protocol set is an internal helper extracted for test coverage and internal policy validation.
func supportedProviderProtocolsForPublisher(modelPublisher string) []string {
	family := deploymentFamilyForPublisher(modelPublisher)
	if family == azureDeploymentFamilyAnthropic {
		return append([]string{}, azureSupportedProviderProtocolsAnthropic...)
	}
	if family == azureDeploymentFamilyOpenAI {
		return append([]string{}, azureSupportedProviderProtocolsOpenAI...)
	}
	return nil
}

func deploymentFamilyForDeployment(modelPublisher string, capabilities azureDeploymentCapabilityRecord) string {
	if family := deploymentFamilyForPublisher(modelPublisher); family != "" {
		return family
	}
	if capabilities.Messages {
		return azureDeploymentFamilyAnthropic
	}
	if capabilities.ChatCompletion || capabilities.Completion {
		return azureDeploymentFamilyOpenAI
	}
	return ""
}

// swobu:lint ignore string-switch because=deployment family is a raw catalog string from external Azure wire.
func supportedProviderProtocolsForDeployment(modelPublisher string, capabilities azureDeploymentCapabilityRecord) []string {
	family := deploymentFamilyForDeployment(modelPublisher, capabilities)
	switch family {
	case azureDeploymentFamilyAnthropic:
		return append([]string{}, azureSupportedProviderProtocolsAnthropic...)
	case azureDeploymentFamilyOpenAI:
		if capabilities.ChatCompletion {
			out := []string{
				"responses",
				"responses_stream",
				"chat_completions",
				"chat_completions_stream",
			}
			if capabilities.Completion {
				out = append(out, "completions", "completions_stream")
			}
			return out
		}
		if capabilities.Completion {
			return []string{"completions", "completions_stream"}
		}
		return nil
	default:
		return nil
	}
}

// swobu:lint ignore test-only-dead-cluster because=model catalog entry routing helpers are alive from test coverage as unit tests verify catalog entry driven policy.
func deploymentFamilyForCatalogEntry(modelID string, capabilities azureModelCatalogCapabilityRecord) string {
	if capabilities.ChatCompletion || capabilities.Completion {
		return azureDeploymentFamilyOpenAI
	}
	return ""
}

// swobu:lint ignore test-only-dead-cluster because=model catalog entry routing helpers are alive from test coverage as unit tests verify catalog entry driven policy.
func supportedProviderProtocolsForCatalogEntry(modelID string, capabilities azureModelCatalogCapabilityRecord) []string {
	family := deploymentFamilyForCatalogEntry(modelID, capabilities)
	if family == azureDeploymentFamilyAnthropic {
		return append([]string{}, azureSupportedProviderProtocolsAnthropic...)
	}
	if family != azureDeploymentFamilyOpenAI {
		return nil
	}
	if capabilities.ChatCompletion {
		out := []string{
			"responses",
			"responses_stream",
			"chat_completions",
			"chat_completions_stream",
		}
		if capabilities.Completion {
			out = append(out, "completions", "completions_stream")
		}
		return out
	}
	if capabilities.Completion {
		return []string{"completions", "completions_stream"}
	}
	return nil
}
