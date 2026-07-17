package endpointintent

import (
	"errors"
	"testing"
)

func TestNewProviderConfigFromTargetDraft_OpenAICompatibleRequiresEndpoint(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	_, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "openai_compatible",
		Endpoint: ProviderEndpointDraft{
			Kind: EndpointRequiredHTTPBaseURL,
		},
		CredentialRef: "env:LAB_API_KEY",
		ModelID:       "gpt-4.1",
		RouteModelID:  "gpt",
		Rank:          1,
		Weight:        1,
	})
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("openai-compatible without endpoint error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestNewProviderConfigFromTargetDraft_OpenAICompatibleStoresCredentialHeader(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	cfg, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "openai_compatible",
		Endpoint: ProviderEndpointDraft{
			Kind:  EndpointRequiredHTTPBaseURL,
			Value: "https://lab.example/v1",
		},
		CredentialRef:    "env:LAB_API_KEY",
		ProviderProtocol: "chat_completions",
		ModelID:          "gpt-4.1",
		RouteModelID:     "gpt",
		Rank:             2,
		Weight:           3,
		ProviderOptions: ProviderOptionsDraft{
			OpenAICompatible: OpenAICompatibleOptionsDraft{CredentialHeader: "X-API-Key"},
		},
	})
	if err != nil {
		t.Fatalf("NewProviderConfigFromTargetDraft returned error: %v", err)
	}
	if got := cfg.AuthHeader(); got != "X-API-Key" {
		t.Fatalf("auth header = %q, want X-API-Key", got)
	}
	if got := cfg.TargetAlias(); got != "" {
		t.Fatalf("target alias = %q, want empty", got)
	}
	if cfg.TargetRank() != 2 || cfg.TargetWeight() != 3 {
		t.Fatalf("rank/weight = %d/%d, want 2/3", cfg.TargetRank(), cfg.TargetWeight())
	}
}

func TestNewProviderConfigFromTargetDraft_OpenAICompatibleLoopbackAllowsNoCredential(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	cfg, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "openai_compatible",
		Endpoint: ProviderEndpointDraft{
			Kind:  EndpointRequiredHTTPBaseURL,
			Value: "http://127.0.0.1:8080/v1",
		},
		ProviderProtocol: "chat_completions",
		ModelID:          "gpt-4.1",
		RouteModelID:     "gpt",
		Rank:             1,
		Weight:           1,
	})
	if err != nil {
		t.Fatalf("NewProviderConfigFromTargetDraft returned error: %v", err)
	}
	if got := cfg.CredentialRef(); got != "" {
		t.Fatalf("credential ref = %q, want empty", got)
	}
}

func TestNewProviderConfigFromTargetDraft_RejectsProviderSpecificOptionsForOtherProviders(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	_, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "openai",
		Endpoint: ProviderEndpointDraft{
			Kind: EndpointDefaultHTTPBaseURL,
		},
		CredentialRef: "env:OPENAI_API_KEY",
		ModelID:       "gpt-4.1",
		RouteModelID:  "gpt",
		Rank:          1,
		Weight:        1,
		ProviderOptions: ProviderOptionsDraft{
			OpenAICompatible: OpenAICompatibleOptionsDraft{CredentialHeader: "X-API-Key"},
		},
	})
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("openai with OpenAI-compatible options error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestNewProviderConfigFromTargetDraft_AzureNormalizesProjectEndpoint(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	cfg, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "azure",
		Endpoint: ProviderEndpointDraft{
			Kind:  EndpointAzureResourceLocator,
			Value: "https://example-resource.services.ai.azure.com/api/projects/example",
		},
		CredentialRef:    "env:AZURE_OPENAI_API_KEY",
		ProviderProtocol: "responses_stream",
		ModelID:          "kimi",
		RouteModelID:     "gpt",
		Rank:             1,
		Weight:           1,
	})
	if err != nil {
		t.Fatalf("NewProviderConfigFromTargetDraft returned error: %v", err)
	}
	if got := cfg.BaseURL(); got != "https://example-resource.services.ai.azure.com/api/projects/example" {
		t.Fatalf("base URL = %q, want normalized Azure project endpoint", got)
	}
}

func TestNewProviderConfigFromTargetDraft_AzureRequiresProjectEndpoint(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	_, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "azure",
		Endpoint: ProviderEndpointDraft{
			Kind: EndpointAzureResourceLocator,
		},
		CredentialRef:    "env:AZURE_OPENAI_API_KEY",
		ProviderProtocol: "responses_stream",
		ModelID:          "kimi",
		RouteModelID:     "gpt",
		Rank:             1,
		Weight:           1,
	})
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("azure without project endpoint error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestNewProviderConfigFromTargetDraft_BedrockRequiresEndpoint(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	_, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "bedrock",
		Endpoint: ProviderEndpointDraft{
			Kind: EndpointRequiredHTTPBaseURL,
		},
		ProviderProtocol: "messages_stream",
		ModelID:          "anthropic.claude-3-7-sonnet",
		RouteModelID:     "gpt",
		Rank:             1,
		Weight:           1,
	})
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("bedrock without endpoint error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestNewProviderConfigFromTargetDraft_BedrockAWSProfileRequiresProfileName(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	_, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "bedrock",
		Endpoint: ProviderEndpointDraft{
			Kind: EndpointRequiredHTTPBaseURL,
		},
		ProviderProtocol: "messages_stream",
		ModelID:          "anthropic.claude-3-7-sonnet",
		RouteModelID:     "gpt",
		Rank:             1,
		Weight:           1,
		ProviderOptions: ProviderOptionsDraft{
			Bedrock: BedrockOptionsDraft{AuthMode: "aws_profile", Region: "eu-west-2"},
		},
	})
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("bedrock aws_profile without profile name error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestNewProviderConfigFromTargetDraft_BedrockStoresAWSProfileAndAWSEnvModes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		mode           string
		profileName    string
		wantCredential string
	}{
		{name: "aws_profile", mode: "aws_profile", profileName: "work-prod", wantCredential: "profile:work-prod"},
		{name: "aws_env_session", mode: "aws_env_session", wantCredential: ""},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ref, _ := ParseProviderConfigRef("cfg-a")
			cfg, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
				ProviderSpec: "bedrock",
				Endpoint: ProviderEndpointDraft{
					Kind: EndpointRequiredHTTPBaseURL,
				},
				ProviderProtocol: "messages_stream",
				ModelID:          "anthropic.claude-3-7-sonnet",
				RouteModelID:     "gpt",
				Rank:             1,
				Weight:           1,
				ProviderOptions: ProviderOptionsDraft{
					Bedrock: BedrockOptionsDraft{AuthMode: tt.mode, Region: "eu-west-2", ProfileName: tt.profileName},
				},
			})
			if err != nil {
				t.Fatalf("NewProviderConfigFromTargetDraft returned error: %v", err)
			}
			if got := cfg.AuthMode(); got != tt.mode {
				t.Fatalf("auth mode = %q, want %q", got, tt.mode)
			}
			if got := cfg.CredentialRef(); got != tt.wantCredential {
				t.Fatalf("credential ref = %q, want %q", got, tt.wantCredential)
			}
			if got := cfg.BaseURL(); got != "https://bedrock-mantle.eu-west-2.api.aws/v1" {
				t.Fatalf("base URL = %q, want derived Mantle endpoint", got)
			}
		})
	}
}

func TestNewProviderConfigFromTargetDraft_BedrockBearerTokenRequiresCredential(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	_, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "bedrock",
		Endpoint: ProviderEndpointDraft{
			Kind: EndpointRequiredHTTPBaseURL,
		},
		ProviderProtocol: "messages_stream",
		ModelID:          "anthropic.claude-3-7-sonnet",
		RouteModelID:     "gpt",
		Rank:             1,
		Weight:           1,
		ProviderOptions: ProviderOptionsDraft{
			Bedrock: BedrockOptionsDraft{AuthMode: "env", Region: "eu-west-2"},
		},
	})
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("bedrock bearer token without credential error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestNewProviderConfigFromTargetDraft_BedrockStoresBearerTokenCredential(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	cfg, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "bedrock",
		Endpoint: ProviderEndpointDraft{
			Kind: EndpointRequiredHTTPBaseURL,
		},
		CredentialRef:    "env:AWS_BEARER_TOKEN_BEDROCK",
		ProviderProtocol: "messages_stream",
		ModelID:          "anthropic.claude-3-7-sonnet",
		RouteModelID:     "gpt",
		Rank:             1,
		Weight:           1,
		ProviderOptions: ProviderOptionsDraft{
			Bedrock: BedrockOptionsDraft{AuthMode: "env", Region: "eu-west-2"},
		},
	})
	if err != nil {
		t.Fatalf("NewProviderConfigFromTargetDraft returned error: %v", err)
	}
	if got := cfg.AuthMode(); got != "env" {
		t.Fatalf("auth mode = %q, want env", got)
	}
	if got := cfg.CredentialRef(); got != "env:AWS_BEARER_TOKEN_BEDROCK" {
		t.Fatalf("credential ref = %q, want bearer token env ref", got)
	}
	if got := cfg.BaseURL(); got != "https://bedrock-mantle.eu-west-2.api.aws/v1" {
		t.Fatalf("base URL = %q, want derived Mantle endpoint", got)
	}
}

func TestNewProviderConfigFromTargetDraft_BedrockRejectsMismatchedRegionAndEndpoint(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	_, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "bedrock",
		Endpoint: ProviderEndpointDraft{
			Kind:  EndpointRequiredHTTPBaseURL,
			Value: "https://bedrock-mantle.us-east-1.api.aws/v1",
		},
		ProviderProtocol: "messages_stream",
		ModelID:          "anthropic.claude-3-7-sonnet",
		RouteModelID:     "gpt",
		Rank:             1,
		Weight:           1,
		ProviderOptions: ProviderOptionsDraft{
			Bedrock: BedrockOptionsDraft{AuthMode: "aws_profile", Region: "eu-west-2"},
		},
	})
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("bedrock mismatched region/endpoint error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestNewProviderConfigFromTargetDraft_RejectsEmptyProtocol(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	_, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "openai",
		Endpoint: ProviderEndpointDraft{
			Kind: EndpointDefaultHTTPBaseURL,
		},
		CredentialRef: "env:OPENAI_API_KEY",
		ModelID:       "gpt-4.1",
		RouteModelID:  "gpt",
		Rank:          1,
		Weight:        1,
	})
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("empty protocol error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestNewProviderConfigFromTargetDraft_RejectsInvalidProtocol(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	_, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "openai",
		Endpoint: ProviderEndpointDraft{
			Kind: EndpointDefaultHTTPBaseURL,
		},
		CredentialRef:    "env:OPENAI_API_KEY",
		ProviderProtocol: "invalid",
		ModelID:          "gpt-4.1",
		RouteModelID:     "gpt",
		Rank:             1,
		Weight:           1,
	})
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("invalid protocol error = %v, want ErrInvalidProviderConfig", err)
	}
}

func TestNewProviderConfigFromTargetDraft_RejectsInvalidRankWeight(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	base := TargetDraft{
		ProviderSpec: "openai",
		Endpoint: ProviderEndpointDraft{
			Kind: EndpointDefaultHTTPBaseURL,
		},
		CredentialRef: "env:OPENAI_API_KEY",
		ModelID:       "gpt-4.1",
		RouteModelID:  "gpt",
		Rank:          1,
		Weight:        1,
	}

	for name, mutate := range map[string]func(*TargetDraft){
		"zero rank":       func(d *TargetDraft) { d.Rank = 0 },
		"negative rank":   func(d *TargetDraft) { d.Rank = -1 },
		"zero weight":     func(d *TargetDraft) { d.Weight = 0 },
		"negative weight": func(d *TargetDraft) { d.Weight = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			draft := base
			mutate(&draft)
			_, err := NewProviderConfigFromTargetDraft(ref, draft)
			if !errors.Is(err, ErrInvalidProviderConfig) {
				t.Fatalf("error = %v, want ErrInvalidProviderConfig", err)
			}
		})
	}
}

func TestNewProviderConfigFromTargetDraft_OpenAICredentialIsExplicit(t *testing.T) {
	t.Parallel()

	ref, _ := ParseProviderConfigRef("cfg-a")
	_, err := NewProviderConfigFromTargetDraft(ref, TargetDraft{
		ProviderSpec: "openai",
		Endpoint: ProviderEndpointDraft{
			Kind: EndpointDefaultHTTPBaseURL,
		},
		ProviderProtocol: "responses_stream",
		ModelID:          "gpt-4.1",
		RouteModelID:     "gpt",
		Rank:             1,
		Weight:           1,
	})
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("openai without credential ref error = %v, want ErrInvalidProviderConfig", err)
	}
}
