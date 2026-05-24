package routing

import "testing"

func TestResolveOpenAICompatibleBedrockBaseURL_AutoSetsMantleForBedrockEnvToken(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_DEFAULT_REGION", "")
	got := resolveOpenAICompatibleBedrockBaseURL("openai_compatible", "AWS_BEARER_TOKEN_BEDROCK", "")
	want := "https://bedrock-mantle.us-east-1.api.aws/v1"
	if got != want {
		t.Fatalf("baseURL=%q want %q", got, want)
	}
}

func TestResolveOpenAICompatibleBedrockBaseURL_PreservesExistingBaseURL(t *testing.T) {
	existing := "https://custom.example/v1"
	got := resolveOpenAICompatibleBedrockBaseURL("openai_compatible", "AWS_BEARER_TOKEN_BEDROCK", existing)
	if got != existing {
		t.Fatalf("baseURL=%q want %q", got, existing)
	}
}

func TestResolveOpenAICompatibleBedrockBaseURL_NonBedrockEnvKeepsBaseURL(t *testing.T) {
	got := resolveOpenAICompatibleBedrockBaseURL("openai_compatible", "OPENAI_API_KEY", "")
	if got != "" {
		t.Fatalf("baseURL=%q want empty", got)
	}
}
