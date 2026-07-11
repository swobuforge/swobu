package model

import (
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
		BaseURL:       "https://bedrock-runtime.eu-west-1.amazonaws.com/openai/v1",
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

func TestEvaluateModelSelectionReadiness_AuthFailureNormalizesOperatorMessage(t *testing.T) {
	t.Parallel()

	errText := "BAD_ENDPOINT: bedrock API key env var is missing: AWS_BEARER_TOKEN_BEDROCK"
	got := EvaluateModelSelectionGateState(ModelSelectionReadinessGateInput{
		ProviderSpec:      "bedrock",
		BaseURL:           "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1",
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

func TestEvaluateModelSelectionReadiness_ProviderAuthVariantMatrix_NoEmptyBlockedMessage(t *testing.T) {
	t.Parallel()

	specs := profile.SupportedSpecs()
	for _, spec := range specs {
		modes := profile.AllowedAuthModesForSpec(spec)
		if len(modes) == 0 {
			t.Fatalf("provider %q has no auth modes", spec)
		}
		for _, mode := range modes {
			ref := string(mode.Variant)
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
				t.Fatalf("provider=%q variant=%q blocked with empty message", spec, mode.Variant)
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
