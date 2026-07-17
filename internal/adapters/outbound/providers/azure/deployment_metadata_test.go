package azure

import "testing"

func TestDeploymentMetadata_PublisherOnlyProtocolPolicy(t *testing.T) {
	t.Parallel()

	for _, publisher := range []string{"", "OpenAI", "DeepSeek", "xAI", "MoonshotAI", "unknown"} {
		if got := deploymentFamilyForDeployment(publisher); got != azureDeploymentFamilyOpenAI {
			t.Fatalf("publisher %q family=%q want %q", publisher, got, azureDeploymentFamilyOpenAI)
		}
		protocols := supportedProviderProtocolsForDeployment(publisher)
		if len(protocols) != len(azureSupportedProviderProtocolsOpenAI) {
			t.Fatalf("publisher %q protocols=%v want %v", publisher, protocols, azureSupportedProviderProtocolsOpenAI)
		}
		if got := defaultProviderProtocolForDeployment(publisher); got != "responses" {
			t.Fatalf("publisher %q default protocol=%q want responses", publisher, got)
		}
	}

	for _, publisher := range []string{"Anthropic", "anthropic", " ANTHROPIC "} {
		if got := deploymentFamilyForDeployment(publisher); got != azureDeploymentFamilyAnthropic {
			t.Fatalf("publisher %q family=%q want %q", publisher, got, azureDeploymentFamilyAnthropic)
		}
		protocols := supportedProviderProtocolsForDeployment(publisher)
		if len(protocols) != len(azureSupportedProviderProtocolsAnthropic) {
			t.Fatalf("publisher %q protocols=%v want %v", publisher, protocols, azureSupportedProviderProtocolsAnthropic)
		}
		if got := defaultProviderProtocolForDeployment(publisher); got != "messages" {
			t.Fatalf("publisher %q default protocol=%q want messages", publisher, got)
		}
	}
}
