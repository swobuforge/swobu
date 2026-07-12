package routing

import "testing"

func TestResolveBedrockMantleBaseURL_AutoSetsMantleForBedrockProvider(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_DEFAULT_REGION", "")
	got := resolveBedrockMantleBaseURL("bedrock", "", "")
	want := "https://bedrock-mantle.us-east-1.api.aws/v1"
	if got != want {
		t.Fatalf("baseURL=%q want %q", got, want)
	}
}

func TestResolveBedrockMantleBaseURL_AutoSetsMantleForBedrockEnvToken(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_DEFAULT_REGION", "")
	got := resolveBedrockMantleBaseURL("openai_compatible", "AWS_BEARER_TOKEN_BEDROCK", "")
	want := "https://bedrock-mantle.us-east-1.api.aws/v1"
	if got != want {
		t.Fatalf("baseURL=%q want %q", got, want)
	}
}

func TestResolveBedrockMantleBaseURL_PreservesExistingBaseURL(t *testing.T) {
	existing := "https://custom.example/v1"
	got := resolveBedrockMantleBaseURL("bedrock", "AWS_BEARER_TOKEN_BEDROCK", existing)
	if got != existing {
		t.Fatalf("baseURL=%q want %q", got, existing)
	}
}

func TestResolveBedrockMantleBaseURL_NonBedrockEnvKeepsBaseURL(t *testing.T) {
	got := resolveBedrockMantleBaseURL("openai_compatible", "OPENAI_API_KEY", "")
	if got != "" {
		t.Fatalf("baseURL=%q want empty", got)
	}
}
