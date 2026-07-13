package azure

import "testing"

func TestDeploymentMetadata_UnknownPublisherFailsClosed(t *testing.T) {
	t.Parallel()

	if got := deploymentFamilyForPublisher(""); got != "" {
		t.Fatalf("empty publisher family = %q, want empty", got)
	}
	if got := deploymentFamilyForPublisher("unknown"); got != "" {
		t.Fatalf("unknown publisher family = %q, want empty", got)
	}
	if got := supportedProviderProtocolsForPublisher(""); len(got) != 0 {
		t.Fatalf("empty publisher protocols = %v, want empty", got)
	}
}

func TestDeploymentMetadata_PublisherFamiliesAndProtocols(t *testing.T) {
	t.Parallel()

	for _, publisher := range []string{"openai", "DeepSeek", "xAI"} {
		if got := deploymentFamilyForPublisher(publisher); got != azureDeploymentFamilyOpenAI {
			t.Fatalf("publisher %q family=%q want %q", publisher, got, azureDeploymentFamilyOpenAI)
		}
		protocols := supportedProviderProtocolsForPublisher(publisher)
		if len(protocols) != len(azureSupportedProviderProtocolsOpenAI) {
			t.Fatalf("publisher %q protocols=%v want %v", publisher, protocols, azureSupportedProviderProtocolsOpenAI)
		}
		if protocols[0] != "responses" || protocols[1] != "responses_stream" {
			t.Fatalf("publisher %q protocols=%v want responses first", publisher, protocols)
		}
	}

	if got := deploymentFamilyForPublisher("anthropic"); got != azureDeploymentFamilyAnthropic {
		t.Fatalf("anthropic family=%q want %q", got, azureDeploymentFamilyAnthropic)
	}
	anthropic := supportedProviderProtocolsForPublisher("anthropic")
	if len(anthropic) != len(azureSupportedProviderProtocolsAnthropic) {
		t.Fatalf("anthropic protocols=%v want %v", anthropic, azureSupportedProviderProtocolsAnthropic)
	}
}

func TestDeploymentMetadata_DeploymentCapabilitiesRefineProtocols(t *testing.T) {
	t.Parallel()

	openAIChat := supportedProviderProtocolsForDeployment("xAI", azureDeploymentCapabilityRecord{ChatCompletion: true})
	if len(openAIChat) != 4 || openAIChat[0] != "responses" || openAIChat[2] != "chat_completions" {
		t.Fatalf("openai chat protocols=%v want responses/chat_completions", openAIChat)
	}

	openAICompletionOnly := supportedProviderProtocolsForDeployment("xAI", azureDeploymentCapabilityRecord{Completion: true})
	if len(openAICompletionOnly) != 2 || openAICompletionOnly[0] != "completions" || openAICompletionOnly[1] != "completions_stream" {
		t.Fatalf("openai completion protocols=%v want completions pair", openAICompletionOnly)
	}

	defaultProtocol := defaultProviderProtocolForDeployment("xAI", azureDeploymentCapabilityRecord{ChatCompletion: true})
	if defaultProtocol != "responses" {
		t.Fatalf("default protocol=%q want responses", defaultProtocol)
	}

	anthropic := supportedProviderProtocolsForDeployment("Anthropic", azureDeploymentCapabilityRecord{Messages: true})
	if len(anthropic) != len(azureSupportedProviderProtocolsAnthropic) {
		t.Fatalf("anthropic deployment protocols=%v want %v", anthropic, azureSupportedProviderProtocolsAnthropic)
	}
	if got := defaultProviderProtocolForDeployment("Anthropic", azureDeploymentCapabilityRecord{Messages: true}); got != "messages" {
		t.Fatalf("anthropic default protocol=%q want messages", got)
	}
}

func TestDeploymentMetadata_CatalogEntryProtocolsFollowCapabilities(t *testing.T) {
	t.Parallel()

	chat := azureModelCatalogCapabilityRecord{Inference: true, ChatCompletion: true}
	got := supportedProviderProtocolsForCatalogEntry("gpt-4o-mini-2024-07-18", chat)
	if len(got) < 4 {
		t.Fatalf("chat-capable protocols=%v want openai family", got)
	}
	if got[0] != "responses" || got[2] != "chat_completions" {
		t.Fatalf("chat-capable protocols=%v want responses/chat_completions", got)
	}

	completionOnly := azureModelCatalogCapabilityRecord{Inference: true, Completion: true}
	got = supportedProviderProtocolsForCatalogEntry("text-davinci-003", completionOnly)
	if len(got) != 2 || got[0] != "completions" || got[1] != "completions_stream" {
		t.Fatalf("completion-only protocols=%v want completions pair", got)
	}
}

func TestDeploymentMetadata_CatalogEntryUnknownCapabilitiesFailClosed(t *testing.T) {
	t.Parallel()

	got := supportedProviderProtocolsForCatalogEntry("whisper-001", azureModelCatalogCapabilityRecord{Inference: true})
	if len(got) != 0 {
		t.Fatalf("unknown capabilities protocols=%v want empty", got)
	}
	if family := deploymentFamilyForCatalogEntry("whisper-001", azureModelCatalogCapabilityRecord{Inference: true}); family != "" {
		t.Fatalf("unknown capabilities family=%q want empty", family)
	}
}
