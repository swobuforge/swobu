package adapters

import (
	"regexp"
	"strings"
	"testing"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/features/target_config"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestDeepSeekOperatorConnectionSurvivesMountedTargetEdit(t *testing.T) {
	const credential = "file:/home/metrofun/.config/deepseek.key"
	operatorTarget := workspaceapi.Target{
		ID: "tgt_c5ca3ff9-222e-42c1-9c4f-74eb2d240f35", Model: "deepseek-v4-pro", Provider: "deepseek",
		Connection: workspaceapi.Connection{DeepSeek: &workspaceapi.CredentialConnection{Credential: credential}},
	}
	projected, err := targetFromWorkspaceTarget(operatorTarget)
	if err != nil {
		t.Fatal(err)
	}
	if projected.CredentialRef != credential {
		t.Fatalf("Cockpit credential = %q, want exact operator credential %q", projected.CredentialRef, credential)
	}
	route := readmodel.RouteReadModel{ID: "worker", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{projected}}}}
	config := target_config.NewEditTargetConfig("demo", route, projected, nil, nil)
	config.Open()
	frame := testkit.RenderMountedTrimmed(t, config, 100, 18)
	if !strings.Contains(frame, "credential        file · /home/metrofun/.config/deepseek.key") {
		t.Fatalf("mounted DeepSeek edit lost configured credential:\n%s", frame)
	}
	if strings.Contains(frame, "credential        required") || strings.Contains(frame, "model             waiting for setup") {
		t.Fatalf("mounted DeepSeek edit contradicted configured backend:\n%s", frame)
	}
}

func TestOperatorConnectionFactsSurviveCockpitProjection(t *testing.T) {
	tests := []struct {
		name       string
		connection workspaceapi.Connection
		want       readmodel.TargetReadModel
	}{
		{name: "openai", connection: workspaceapi.Connection{OpenAI: &workspaceapi.CredentialConnection{Credential: "env:OPENAI_API_KEY"}}, want: readmodel.TargetReadModel{Provider: "openai", CredentialRef: "env:OPENAI_API_KEY"}},
		{name: "anthropic", connection: workspaceapi.Connection{Anthropic: &workspaceapi.CredentialConnection{Credential: "env:ANTHROPIC_API_KEY"}}, want: readmodel.TargetReadModel{Provider: "anthropic", CredentialRef: "env:ANTHROPIC_API_KEY"}},
		{name: "deepseek", connection: workspaceapi.Connection{DeepSeek: &workspaceapi.CredentialConnection{Credential: "file:/home/metrofun/.config/deepseek.key"}}, want: readmodel.TargetReadModel{Provider: "deepseek", CredentialRef: "file:/home/metrofun/.config/deepseek.key"}},
		{name: "openrouter", connection: workspaceapi.Connection{OpenRouter: &workspaceapi.CredentialConnection{Credential: "env:OPENROUTER_API_KEY"}}, want: readmodel.TargetReadModel{Provider: "openrouter", CredentialRef: "env:OPENROUTER_API_KEY"}},
		{name: "chatgpt", connection: workspaceapi.Connection{ChatGPT: &workspaceapi.CredentialConnection{Credential: "secret:chatgpt/session"}}, want: readmodel.TargetReadModel{Provider: "chatgpt", CredentialRef: "secret:chatgpt/session"}},
		{name: "zai", connection: workspaceapi.Connection{ZAI: &workspaceapi.ZAIConnection{Access: "coding_plan", Credential: "env:ZAI_API_KEY"}}, want: readmodel.TargetReadModel{Provider: "zai", ZAIAccess: "coding_plan", CredentialRef: "env:ZAI_API_KEY"}},
		{name: "ollama", connection: workspaceapi.Connection{Ollama: &workspaceapi.OllamaConnection{BaseURL: "http://127.0.0.1:11434/v1"}}, want: readmodel.TargetReadModel{Provider: "ollama", BaseURL: "http://127.0.0.1:11434/v1"}},
		{name: "lmstudio", connection: workspaceapi.Connection{LMStudio: &workspaceapi.EndpointCredentialConnection{BaseURL: "http://127.0.0.1:1234/v1", Credential: "env:LM_API_TOKEN"}}, want: readmodel.TargetReadModel{Provider: "lmstudio", BaseURL: "http://127.0.0.1:1234/v1", CredentialRef: "env:LM_API_TOKEN"}},
		{name: "vllm", connection: workspaceapi.Connection{VLLM: &workspaceapi.EndpointCredentialConnection{BaseURL: "http://127.0.0.1:8000/v1", Credential: "env:VLLM_API_KEY"}}, want: readmodel.TargetReadModel{Provider: "vllm", BaseURL: "http://127.0.0.1:8000/v1", CredentialRef: "env:VLLM_API_KEY"}},
		{name: "azure", connection: workspaceapi.Connection{Azure: &workspaceapi.AzureConnection{ProjectEndpoint: "https://example.services.ai.azure.com/api/projects/demo", Credential: "env:AZURE_KEY"}}, want: readmodel.TargetReadModel{Provider: "azure", BaseURL: "https://example.services.ai.azure.com/api/projects/demo", CredentialRef: "env:AZURE_KEY"}},
		{name: "bedrock", connection: workspaceapi.Connection{Bedrock: &workspaceapi.BedrockConnection{Region: "eu-west-2", Endpoint: "https://bedrock-mantle.eu-west-2.api.aws/v1", Credential: "env:BEDROCK_KEY"}}, want: readmodel.TargetReadModel{Provider: "bedrock", BaseURL: "https://bedrock-mantle.eu-west-2.api.aws/v1", BedrockRegion: "eu-west-2", CredentialRef: "env:BEDROCK_KEY"}},
		{name: "custom", connection: workspaceapi.Connection{Custom: &workspaceapi.CustomConnection{BaseURL: "https://example.test/v1", Header: &workspaceapi.CustomHeader{Name: "x-api-key", Credential: "env:CUSTOM_KEY"}}}, want: readmodel.TargetReadModel{Provider: "custom", BaseURL: "https://example.test/v1", AuthHeader: "x-api-key", CredentialRef: "env:CUSTOM_KEY"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := targetFromWorkspaceTarget(workspaceapi.Target{ID: "target", Model: "model", Connection: test.connection})
			if err != nil {
				t.Fatal(err)
			}
			if got.Provider != test.want.Provider || got.BaseURL != test.want.BaseURL || got.CredentialRef != test.want.CredentialRef || got.ZAIAccess != test.want.ZAIAccess || got.AuthHeader != test.want.AuthHeader || got.BedrockRegion != test.want.BedrockRegion {
				t.Fatalf("projection = %#v, want connection facts %#v", got, test.want)
			}
		})
	}
}

func TestNewTargetIDUsesOpaqueTypedUUID(t *testing.T) {
	id, err := newTargetID()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^tgt_[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) {
		t.Fatalf("target ID = %q", id)
	}
}

func TestTargetFromSaveRequestProjectsValidatedConnection(t *testing.T) {
	bedrockRegion, err := routing.ParseBedrockRegion("eu-west-1")
	if err != nil {
		t.Fatal(err)
	}
	customAuth, err := routing.NewCustomHeaderAuth("X-API-Key", "env:CUSTOM_KEY")
	if err != nil {
		t.Fatal(err)
	}
	constructors := []struct {
		name string
		make func() (routing.Connection, error)
		arm  func(targetConnection) bool
	}{
		{"openai", func() (routing.Connection, error) {
			return routing.NewAPIKeyConnection(routing.ProviderOpenAI, "env:OPENAI_API_KEY")
		}, func(c targetConnection) bool { return c.openAI }},
		{"anthropic", func() (routing.Connection, error) {
			return routing.NewAPIKeyConnection(routing.ProviderAnthropic, "env:ANTHROPIC_API_KEY")
		}, func(c targetConnection) bool { return c.anthropic }},
		{"openrouter", func() (routing.Connection, error) {
			return routing.NewAPIKeyConnection(routing.ProviderOpenRouter, "env:OPENROUTER_API_KEY")
		}, func(c targetConnection) bool { return c.openRouter }},
		{"zai", func() (routing.Connection, error) {
			return routing.NewZAIConnection(routing.ZAIAccessCodingPlan, "env:ZAI_API_KEY")
		}, func(c targetConnection) bool { return c.zai }},
		{"chatgpt", func() (routing.Connection, error) {
			return routing.NewAPIKeyConnection(routing.ProviderChatGPT, "secret:chatgpt/session")
		}, func(c targetConnection) bool { return c.chatGPT }},
		{"ollama", func() (routing.Connection, error) { return routing.NewOllamaConnection("http://127.0.0.1:11434") }, func(c targetConnection) bool { return c.ollama }},
		{"lm studio", func() (routing.Connection, error) {
			return routing.NewEndpointCredentialConnection(routing.ProviderLMStudio, "http://127.0.0.1:1234/v1", "env:LM_API_TOKEN")
		}, func(c targetConnection) bool { return c.lmStudio }},
		{"azure", func() (routing.Connection, error) {
			return routing.NewAzureConnection("https://example.services.ai.azure.com/api/projects/demo", "env:AZURE_OPENAI_API_KEY")
		}, func(c targetConnection) bool { return c.azure }},
		{"bedrock", func() (routing.Connection, error) {
			return routing.NewBedrockConnection(bedrockRegion, "https://bedrock-mantle.eu-west-2.api.aws/v1", "")
		}, func(c targetConnection) bool { return c.bedrock }},
		{"custom", func() (routing.Connection, error) {
			return routing.NewCustomConnection("https://example.com/v1", customAuth)
		}, func(c targetConnection) bool { return c.custom }},
	}
	for _, test := range constructors {
		t.Run(test.name, func(t *testing.T) {
			connection, err := test.make()
			if err != nil {
				t.Fatal(err)
			}
			target, err := targetFromSaveRequest(ports.SaveTargetRequest{
				ModelID:    "model",
				Protocol:   "responses",
				Connection: connection,
			}, "target")
			if err != nil {
				t.Fatal(err)
			}
			arms := targetConnection{
				openAI: target.Connection.OpenAI != nil, anthropic: target.Connection.Anthropic != nil,
				openRouter: target.Connection.OpenRouter != nil, zai: target.Connection.ZAI != nil, chatGPT: target.Connection.ChatGPT != nil,
				ollama: target.Connection.Ollama != nil, lmStudio: target.Connection.LMStudio != nil, azure: target.Connection.Azure != nil,
				bedrock: target.Connection.Bedrock != nil, custom: target.Connection.Custom != nil,
			}
			if !test.arm(arms) {
				t.Fatalf("projected connection arms = %#v", arms)
			}
		})
	}
}

type targetConnection struct {
	openAI, anthropic, openRouter, zai, chatGPT, ollama, lmStudio, azure, bedrock, custom bool
}

func TestPlacementFromReadModelHasOnlyOptionalBalanceTarget(t *testing.T) {
	fallback := placementFromReadModel(readmodel.PlacementOptionReadModel{Kind: readmodel.PlacementFallback})
	if fallback.BalanceWith != nil {
		t.Fatalf("fallback placement = %#v", fallback)
	}
	balance := placementFromReadModel(readmodel.PlacementOptionReadModel{Kind: readmodel.PlacementBalance, PeerTargetID: "a"})
	if balance.BalanceWith == nil || *balance.BalanceWith != "a" {
		t.Fatalf("balance placement = %#v", balance)
	}
}
