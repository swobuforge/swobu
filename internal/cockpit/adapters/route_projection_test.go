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
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestDeepSeekOperatorConnectionSurvivesMountedTargetEdit(t *testing.T) {
	const credential = "file:/home/metrofun/.config/deepseek.key"
	operatorTarget := workspaceapi.Target{
		ID: "tgt_c5ca3ff9-222e-42c1-9c4f-74eb2d240f35", Model: "deepseek-v4-pro", Provider: "deepseek",
		Connection: workspaceapi.StandardConnection("deepseek", "", credential),
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
		{"openai", workspaceapi.StandardConnection("openai", "", "env:OPENAI_API_KEY"), readmodel.TargetReadModel{Provider: "openai", CredentialRef: "env:OPENAI_API_KEY"}},
		{"anthropic", workspaceapi.StandardConnection("anthropic", "", "env:ANTHROPIC_API_KEY"), readmodel.TargetReadModel{Provider: "anthropic", CredentialRef: "env:ANTHROPIC_API_KEY"}},
		{"deepseek", workspaceapi.StandardConnection("deepseek", "", "file:/home/metrofun/.config/deepseek.key"), readmodel.TargetReadModel{Provider: "deepseek", CredentialRef: "file:/home/metrofun/.config/deepseek.key"}},
		{"openrouter", workspaceapi.StandardConnection("openrouter", "", "env:OPENROUTER_API_KEY"), readmodel.TargetReadModel{Provider: "openrouter", CredentialRef: "env:OPENROUTER_API_KEY"}},
		{"chatgpt", workspaceapi.StandardConnection("chatgpt", "", "secret:chatgpt/session"), readmodel.TargetReadModel{Provider: "chatgpt", CredentialRef: "secret:chatgpt/session"}},
		{"zai", workspaceapi.ZAIConnectionDocument("coding_plan", "env:ZAI_API_KEY"), readmodel.TargetReadModel{Provider: "zai", ZAIAccess: "coding_plan", CredentialRef: "env:ZAI_API_KEY"}},
		{"ollama", workspaceapi.StandardConnection("ollama", "http://127.0.0.1:11434/v1", ""), readmodel.TargetReadModel{Provider: "ollama", BaseURL: "http://127.0.0.1:11434/v1"}},
		{"lmstudio", workspaceapi.StandardConnection("lmstudio", "http://127.0.0.1:1234/v1", "env:LM_API_TOKEN"), readmodel.TargetReadModel{Provider: "lmstudio", BaseURL: "http://127.0.0.1:1234/v1", CredentialRef: "env:LM_API_TOKEN"}},
		{"vllm", workspaceapi.StandardConnection("vllm", "http://127.0.0.1:8000/v1", "env:VLLM_API_KEY"), readmodel.TargetReadModel{Provider: "vllm", BaseURL: "http://127.0.0.1:8000/v1", CredentialRef: "env:VLLM_API_KEY"}},
		{"azure", workspaceapi.StandardConnection("azure", "https://example.services.ai.azure.com/api/projects/demo", "env:AZURE_KEY"), readmodel.TargetReadModel{Provider: "azure", BaseURL: "https://example.services.ai.azure.com/api/projects/demo", CredentialRef: "env:AZURE_KEY"}},
		{"bedrock", workspaceapi.BedrockConnectionDocument("eu-west-2", "https://bedrock-mantle.eu-west-2.api.aws/v1", "env:BEDROCK_KEY"), readmodel.TargetReadModel{Provider: "bedrock", BaseURL: "https://bedrock-mantle.eu-west-2.api.aws/v1", BedrockRegion: "eu-west-2", CredentialRef: "env:BEDROCK_KEY"}},
		{"custom", workspaceapi.CustomConnectionDocument("https://example.test/v1", &workspaceapi.CustomHeader{Name: "x-api-key", Credential: "env:CUSTOM_KEY"}), readmodel.TargetReadModel{Provider: "custom", BaseURL: "https://example.test/v1", AuthHeader: "x-api-key", CredentialRef: "env:CUSTOM_KEY"}},
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

func TestTargetFromSaveRequestPreservesProviderKeyAcrossAdapters(t *testing.T) {
	bedrockRegion, err := routing.ParseBedrockRegion("eu-west-1")
	if err != nil {
		t.Fatal(err)
	}
	customAuth, err := routing.NewCustomHeaderAuth("X-API-Key", "env:CUSTOM_KEY")
	if err != nil {
		t.Fatal(err)
	}
	standard := func(spec, locator, credential string) func() (routing.Connection, error) {
		return func() (routing.Connection, error) {
			provider, err := routing.ParseProvider(spec, profile.SupportsSpec)
			if err != nil {
				return nil, err
			}
			return routing.NewStandardConnection(provider, locator, credential)
		}
	}
	tests := []struct {
		name string
		make func() (routing.Connection, error)
		want string
	}{
		{"openai", standard("openai", "", "env:OPENAI_API_KEY"), "openai"},
		{"anthropic", standard("anthropic", "", "env:ANTHROPIC_API_KEY"), "anthropic"},
		{"openrouter", standard("openrouter", "", "env:OPENROUTER_API_KEY"), "openrouter"},
		{"chatgpt", standard("chatgpt", "", "secret:chatgpt/session"), "chatgpt"},
		{"ollama", standard("ollama", "http://127.0.0.1:11434", ""), "ollama"},
		{"lmstudio", standard("lmstudio", "http://127.0.0.1:1234/v1", "env:LM_API_TOKEN"), "lmstudio"},
		{"azure", standard("azure", "https://example.services.ai.azure.com/api/projects/demo", "env:AZURE_OPENAI_API_KEY"), "azure"},
		{"zai", func() (routing.Connection, error) {
			provider, _ := routing.ParseProvider("zai", profile.SupportsSpec)
			return routing.NewZAIConnection(provider, routing.ZAIAccessCodingPlan, "env:ZAI_API_KEY")
		}, "zai"},
		{"bedrock", func() (routing.Connection, error) {
			provider, _ := routing.ParseProvider("bedrock", profile.SupportsSpec)
			return routing.NewBedrockConnection(provider, bedrockRegion, "https://bedrock-mantle.eu-west-2.api.aws/v1", "")
		}, "bedrock"},
		{"custom", func() (routing.Connection, error) {
			provider, _ := routing.ParseProvider("custom", profile.SupportsSpec)
			return routing.NewCustomConnection(provider, "https://example.com/v1", customAuth)
		}, "custom"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connection, err := test.make()
			if err != nil {
				t.Fatal(err)
			}
			target, err := targetFromSaveRequest(ports.SaveTargetRequest{ModelID: "model", Protocol: "responses", Connection: connection}, "target")
			if err != nil {
				t.Fatal(err)
			}
			projected, err := target.Connection.RoutingConnection()
			if err != nil || string(projected.Provider()) != test.want {
				t.Fatalf("projected provider = %v, %v; want %q", projected, err, test.want)
			}
		})
	}
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
