package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/profile"
)

func TestEvaluateModelSelectionReadiness_InteractivePendingBlocks(t *testing.T) {
	t.Parallel()

	got := EvaluateModelSelectionGateState(ModelSelectionReadinessGateInput{
		ProviderSpec:            "chatgpt",
		CredentialRef:           "chatgpt_login",
		InteractiveAuthResolved: false,
	})
	if !got.Blocked {
		t.Fatal("expected blocked while interactive auth is unresolved")
	}
	if got.Reason != ModelSelectionBlockInteractiveAuthPending {
		t.Fatalf("reason=%q", got.Reason)
	}
}

func TestEvaluateModelSelectionReadiness_BedrockChainNeedsExplicitProfile(t *testing.T) {
	t.Parallel()

	got := EvaluateModelSelectionGateState(ModelSelectionReadinessGateInput{
		ProviderSpec:  "bedrock",
		BaseURL:       "https://bedrock-mantle.eu-west-1.api.aws/v1",
		CredentialRef: "aws_profile",
	})
	if !got.Blocked {
		t.Fatal("expected blocked when bedrock profile is unresolved")
	}
	if got.Reason != ModelSelectionBlockBedrockProfileMissing {
		t.Fatalf("reason=%q", got.Reason)
	}
	if got.Message != "select AWS profile before loading models" {
		t.Fatalf("message=%q", got.Message)
	}
}

func TestEvaluateModelSelectionReadiness_BedrockProfileUnavailableBlocks(t *testing.T) {
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")
	homeAWS := filepath.Join(home, ".aws")
	if err := os.MkdirAll(homeAWS, 0o755); err != nil {
		t.Fatalf("mkdir home .aws: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeAWS, "config"), []byte("[profile home-bedrock]\nregion = us-west-2\n"), 0o600); err != nil {
		t.Fatalf("write home config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeAWS, "credentials"), []byte("[home-bedrock]\naws_access_key_id = home\naws_secret_access_key = home\n"), 0o600); err != nil {
		t.Fatalf("write home credentials: %v", err)
	}

	active := t.TempDir()
	activeConfig := filepath.Join(active, "config")
	if err := os.WriteFile(activeConfig, []byte("[profile swobu-bedrock]\nregion = us-east-1\n"), 0o600); err != nil {
		t.Fatalf("write active config: %v", err)
	}
	activeCreds := filepath.Join(active, "credentials")
	if err := os.WriteFile(activeCreds, []byte("[swobu-bedrock]\naws_access_key_id = active\naws_secret_access_key = active\n"), 0o600); err != nil {
		t.Fatalf("write active credentials: %v", err)
	}
	t.Setenv("AWS_CONFIG_FILE", activeConfig)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", activeCreds)

	got := EvaluateModelSelectionGateState(ModelSelectionReadinessGateInput{
		ProviderSpec:  "bedrock",
		BaseURL:       "https://bedrock-mantle.us-east-1.api.aws/v1",
		CredentialRef: "profile:home-bedrock",
	})
	if !got.Blocked {
		t.Fatal("expected blocked when the selected Bedrock profile is missing from the active AWS files")
	}
	if got.Reason != ModelSelectionBlockBedrockProfileUnavailable {
		t.Fatalf("reason=%q", got.Reason)
	}
	if got.Message != "bedrock AWS profile \"home-bedrock\" is not available in the active AWS config" {
		t.Fatalf("message=%q", got.Message)
	}
}

func TestEvaluateModelSelectionReadiness_EnvKeyMissingBlocks(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	got := EvaluateModelSelectionGateState(ModelSelectionReadinessGateInput{
		ProviderSpec:  "openai",
		CredentialRef: "env:OPENAI_API_KEY",
	})
	if !got.Blocked {
		t.Fatal("expected blocked when env key is missing")
	}
	if got.Reason != ModelSelectionBlockEnvVarMissing {
		t.Fatalf("reason=%q", got.Reason)
	}
}

func TestEvaluateModelSelectionReadiness_AzureMissingBaseURLBlocksBeforeEnvLookup(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_KEY", "")
	got := EvaluateModelSelectionGateState(ModelSelectionReadinessGateInput{
		ProviderSpec:  "azure",
		CredentialRef: "env:AZURE_OPENAI_API_KEY",
	})
	if !got.Blocked {
		t.Fatal("expected blocked when azure base URL is missing")
	}
	if got.Message != "set backend URL before loading models" {
		t.Fatalf("message=%q", got.Message)
	}
}

func TestEvaluateModelSelectionReadiness_AzureReadyWhenBaseURLSet(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_KEY", "azure-test-key")
	got := EvaluateModelSelectionGateState(ModelSelectionReadinessGateInput{
		ProviderSpec:  "azure",
		BaseURL:       "https://contact-5464-resource.openai.azure.com/openai/v1",
		CredentialRef: "env:AZURE_OPENAI_API_KEY",
	})
	if got.Blocked {
		t.Fatalf("expected unblocked azure readiness, got reason=%q message=%q", got.Reason, got.Message)
	}
}

func TestEvaluateModelSelectionReadiness_AuthFailureNormalizesOperatorMessage(t *testing.T) {
	t.Parallel()

	errText := "BAD_ENDPOINT: bedrock API key env var is missing: AWS_BEARER_TOKEN_BEDROCK"
	got := EvaluateModelSelectionGateState(ModelSelectionReadinessGateInput{
		ProviderSpec:      "bedrock",
		BaseURL:           "https://bedrock-mantle.us-east-1.api.aws/v1",
		CredentialRef:     "env:AWS_BEARER_TOKEN_BEDROCK",
		ModelCatalogError: errText,
	})
	if !got.Blocked {
		t.Fatal("expected blocked on auth failure")
	}
	if got.Reason != ModelSelectionBlockAuthProbeFailed {
		t.Fatalf("reason=%q", got.Reason)
	}
	if got.Message != "bedrock API key env var is missing: AWS_BEARER_TOKEN_BEDROCK" {
		t.Fatalf("message=%q", got.Message)
	}
}

func TestEvaluateModelSelectionReadiness_ProviderAuthModeMatrix_NoEmptyBlockedMessage(t *testing.T) {
	t.Parallel()

	specs := profile.SupportedSpecs()
	for _, spec := range specs {
		modes := profile.AllowedAuthModesForSpec(spec)
		if len(modes) == 0 {
			t.Fatalf("provider %q has no auth modes", spec)
		}
		for _, mode := range modes {
			ref := string(mode.Mode)
			if ref == "" {
				ref = ""
			}
			got := EvaluateModelSelectionGateState(ModelSelectionReadinessGateInput{
				ProviderSpec:            spec,
				BaseURL:                 profile.DefaultExecuteBaseURL(spec),
				CredentialRef:           ref,
				InteractiveAuthResolved: false,
			})
			if got.Blocked && got.Message == "" {
				t.Fatalf("provider=%q mode=%q blocked with empty message", spec, mode.Mode)
			}
		}
	}
}

func TestEvaluateModelSelectionReadiness_ChatGPTTierError_ResolvedCredentialSuggestsSwitchAccount(t *testing.T) {
	t.Parallel()

	got := EvaluateModelSelectionGateState(ModelSelectionReadinessGateInput{
		ProviderSpec:      "chatgpt",
		CredentialRef:     "keychain:chatgpt/default",
		ModelCatalogError: "BAD_ENDPOINT: chatgpt subscription tier could not be resolved from credential",
	})
	if !got.Blocked {
		t.Fatal("expected blocked on tier-resolution failure")
	}
	if got.Reason != ModelSelectionBlockAuthProbeFailed {
		t.Fatalf("reason=%q", got.Reason)
	}
	if got.Message != "signed-in account could not resolve ChatGPT subscription tier; sign in another account" {
		t.Fatalf("message=%q", got.Message)
	}
}

func TestEvaluateModelSelectionReadiness_ChatGPTTierError_InteractiveCredentialSuggestsSignIn(t *testing.T) {
	t.Parallel()

	got := EvaluateModelSelectionGateState(ModelSelectionReadinessGateInput{
		ProviderSpec:      "chatgpt",
		CredentialRef:     "chatgpt_login",
		ModelCatalogError: "BAD_ENDPOINT: chatgpt subscription tier could not be resolved from credential",
	})
	if !got.Blocked {
		t.Fatal("expected blocked on tier-resolution failure")
	}
	if got.Reason != ModelSelectionBlockAuthProbeFailed {
		t.Fatalf("reason=%q", got.Reason)
	}
	if got.Message != "sign in to resolve ChatGPT subscription tier" {
		t.Fatalf("message=%q", got.Message)
	}
}
