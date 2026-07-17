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
	}
	azureSupportedProviderProtocolsAnthropic = []string{
		"messages",
		"messages_stream",
	}
)

func deploymentFamilyForDeployment(modelPublisher string) string {
	publisher := strings.ToLower(strings.TrimSpace(modelPublisher)) // swobu:io-string source=boundary
	if publisher == azureDeploymentFamilyAnthropic {
		return azureDeploymentFamilyAnthropic
	}
	return azureDeploymentFamilyOpenAI
}

func supportedProviderProtocolsForDeployment(modelPublisher string) []string {
	if deploymentFamilyForDeployment(modelPublisher) == azureDeploymentFamilyAnthropic {
		return append([]string{}, azureSupportedProviderProtocolsAnthropic...)
	}
	return append([]string{}, azureSupportedProviderProtocolsOpenAI...)
}
