package adapters

import (
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/routing"
)

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
		{"openai", func() (routing.Connection, error) { return routing.NewOpenAIConnection("env:OPENAI_API_KEY") }, func(c targetConnection) bool { return c.openAI }},
		{"anthropic", func() (routing.Connection, error) { return routing.NewAnthropicConnection("env:ANTHROPIC_API_KEY") }, func(c targetConnection) bool { return c.anthropic }},
		{"openrouter", func() (routing.Connection, error) { return routing.NewOpenRouterConnection("env:OPENROUTER_API_KEY") }, func(c targetConnection) bool { return c.openRouter }},
		{"chatgpt", func() (routing.Connection, error) { return routing.NewChatGPTConnection("secret:chatgpt/session") }, func(c targetConnection) bool { return c.chatGPT }},
		{"ollama", func() (routing.Connection, error) { return routing.NewOllamaConnection("http://127.0.0.1:11434") }, func(c targetConnection) bool { return c.ollama }},
		{"azure", func() (routing.Connection, error) {
			return routing.NewAzureConnection("https://example.services.ai.azure.com/api/projects/demo", "env:AZURE_OPENAI_API_KEY")
		}, func(c targetConnection) bool { return c.azure }},
		{"bedrock", func() (routing.Connection, error) { return routing.NewBedrockConnection(bedrockRegion, "") }, func(c targetConnection) bool { return c.bedrock }},
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
				openRouter: target.Connection.OpenRouter != nil, chatGPT: target.Connection.ChatGPT != nil,
				ollama: target.Connection.Ollama != nil, azure: target.Connection.Azure != nil,
				bedrock: target.Connection.Bedrock != nil, custom: target.Connection.Custom != nil,
			}
			if !test.arm(arms) {
				t.Fatalf("projected connection arms = %#v", arms)
			}
		})
	}
}

type targetConnection struct {
	openAI, anthropic, openRouter, chatGPT, ollama, azure, bedrock, custom bool
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
