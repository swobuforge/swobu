package effect

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func TestArgsToProviderConfig_IgnoresLegacyProtocolTupleInput(t *testing.T) {
	t.Parallel()

	_, err := argsToProviderConfig(stateModel.ProviderConfigSnapshot{
		Ref:           "backend-a",
		ProviderSpec:  "anthropic",
		ModelID:       "claude-sonnet",
		CredentialRef: "env:ANTHROPIC_API_KEY",
	})
	if err != nil {
		t.Fatalf("argsToProviderConfig returned error: %v", err)
	}
}

func TestArgsToProviderConfig_PreservesProviderProtocol(t *testing.T) {
	t.Parallel()

	cfg, err := argsToProviderConfig(stateModel.ProviderConfigSnapshot{
		Ref:              "backend-a",
		ProviderSpec:     "openai",
		ProviderProtocol: "responses_stream",
		ModelID:          "gpt-5.4-mini",
		CredentialRef:    "env:OPENAI_API_KEY",
	})
	if err != nil {
		t.Fatalf("argsToProviderConfig returned error: %v", err)
	}
	if got := cfg.ProviderProtocol(); got != "responses_stream" {
		t.Fatalf("provider protocol=%q want=%q", got, "responses_stream")
	}
}

func TestArgsToProviderConfig_PreservesAutoProviderProtocolForDaemonResolution(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_DEFAULT_REGION", "")

	cfg, err := argsToProviderConfig(stateModel.ProviderConfigSnapshot{
		Ref:              "backend-a",
		ProviderSpec:     "bedrock",
		ProviderProtocol: "auto",
		ModelID:          "qwen.qwen3-coder-next",
		CredentialRef:    "profile:default",
	})
	if err != nil {
		t.Fatalf("argsToProviderConfig returned error: %v", err)
	}
	if got := cfg.ProviderProtocol(); got != "auto" {
		t.Fatalf("provider protocol=%q want=%q", got, "auto")
	}
	if got := cfg.BaseURL(); got != "https://bedrock-mantle.us-east-1.api.aws/v1" {
		t.Fatalf("base URL=%q want bedrock us-east-1 runtime URL", got)
	}
}

func TestArgsToProviderConfig_BedrockRegionDerivesBaseURL(t *testing.T) {
	t.Parallel()

	cfg, err := argsToProviderConfig(stateModel.ProviderConfigSnapshot{
		Ref:           "backend-a",
		ProviderSpec:  "bedrock",
		Region:        "eu-west-2",
		BaseURL:       "",
		CredentialRef: "profile:default",
	})
	if err != nil {
		t.Fatalf("argsToProviderConfig returned error: %v", err)
	}
	if got := cfg.BaseURL(); got != "https://bedrock-mantle.eu-west-2.api.aws/v1" {
		t.Fatalf("base URL=%q want eu-west-2 bedrock runtime URL", got)
	}
}

func TestArgsToProviderConfig_PreservesOpenAICompatibleAuthHeader(t *testing.T) {
	t.Parallel()

	cfg, err := argsToProviderConfig(stateModel.ProviderConfigSnapshot{
		Ref:           "backend-a",
		ProviderSpec:  "openai_compatible",
		BaseURL:       "https://example.test/v1",
		AuthHeader:    "X-Custom-Auth",
		ModelID:       "gpt-4.1-mini",
		CredentialRef: "env:OPENAI_API_KEY",
	})
	if err != nil {
		t.Fatalf("argsToProviderConfig returned error: %v", err)
	}
	if got := cfg.AuthHeader(); got != "X-Custom-Auth" {
		t.Fatalf("auth header=%q want X-Custom-Auth", got)
	}
}

func TestEndpointToSnapshot_PreservesAuthHeader(t *testing.T) {
	t.Parallel()

	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "env:OPENAI_API_KEY")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfg, err = cfg.WithAuthHeader("X-Custom-Auth")
	if err != nil {
		t.Fatalf("WithAuthHeader returned error: %v", err)
	}
	ep, err := endpointintent.NewEndpoint(
		func() endpointintent.EndpointName {
			name, err := endpointintent.ParseEndpointName("acme")
			if err != nil {
				t.Fatalf("ParseEndpointName returned error: %v", err)
			}
			return name
		}(),
		[]endpointintent.ProviderConfig{cfg},
		cfg.Ref(),
	)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	snapshot := endpointToSnapshot(ep)
	if len(snapshot.ProviderConfigs) != 1 {
		t.Fatalf("provider config count=%d want 1", len(snapshot.ProviderConfigs))
	}
	if got := snapshot.ProviderConfigs[0].AuthHeader; got != "X-Custom-Auth" {
		t.Fatalf("snapshot auth header=%q want X-Custom-Auth", got)
	}
}
