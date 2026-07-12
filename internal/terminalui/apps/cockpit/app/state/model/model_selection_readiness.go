package model

import (
	"fmt"
	"os"
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
)

type ModelSelectionBlockReason string

const (
	ModelSelectionBlockNone                      ModelSelectionBlockReason = ""
	ModelSelectionBlockAuthProbeFailed           ModelSelectionBlockReason = "auth_probe_failed"
	ModelSelectionBlockCreateFlowPrerequisite    ModelSelectionBlockReason = "create_flow_prerequisite"
	ModelSelectionBlockInteractiveAuthPending    ModelSelectionBlockReason = "interactive_auth_pending"
	ModelSelectionBlockBedrockProfileMissing     ModelSelectionBlockReason = "bedrock_profile_missing"
	ModelSelectionBlockBedrockProfileUnavailable ModelSelectionBlockReason = "bedrock_profile_unavailable"
	ModelSelectionBlockEnvVarMissing             ModelSelectionBlockReason = "env_var_missing"
	ModelSelectionBlockGenericAuth               ModelSelectionBlockReason = "generic_auth"
)

type ModelSelectionReadinessGateInput struct {
	ProviderSpec            string
	BaseURL                 string
	CredentialRef           string
	ModelCatalogError       string
	CreateDraft             *ProviderConfigSnapshot
	InteractiveAuthResolved bool
}

type ModelSelectionGateState struct {
	Blocked bool
	Reason  ModelSelectionBlockReason
	Message string
}

func EvaluateModelSelectionGateState(input ModelSelectionReadinessGateInput) ModelSelectionGateState {
	provider := strings.TrimSpace(input.ProviderSpec)       // swobu:io-string source=boundary
	baseURL := strings.TrimSpace(input.BaseURL)             // swobu:io-string source=boundary
	credentialRef := strings.TrimSpace(input.CredentialRef) // swobu:io-string source=boundary
	modelErr := strings.TrimSpace(input.ModelCatalogError)  // swobu:io-string source=boundary
	if provider == "" {
		return ModelSelectionGateState{}
	}
	if message := strings.TrimSpace(ProviderModelCatalogAuthFailureMessage(provider, credentialRef, modelErr)); message != "" { // swobu:io-string source=boundary
		return ModelSelectionGateState{Blocked: true, Reason: ModelSelectionBlockAuthProbeFailed, Message: message}
	}
	if input.CreateDraft != nil {
		flow := EvaluateCreateDraftRouteSetup(*input.CreateDraft)
		if flow.ModelState == RouteSetupSlotBlocked {
			return ModelSelectionGateState{
				Blocked: true,
				Reason:  ModelSelectionBlockCreateFlowPrerequisite,
				Message: strings.TrimSpace(flow.ModelBlocker), // swobu:io-string source=boundary
			}
		}
	}
	if ProviderCredentialVariantIsInteractive(provider, credentialRef) && !input.InteractiveAuthResolved {
		return ModelSelectionGateState{
			Blocked: true,
			Reason:  ModelSelectionBlockInteractiveAuthPending,
			Message: "complete browser sign-in to load models",
		}
	}
	if strings.EqualFold(provider, "bedrock") && isBedrockAWSProfileRef(credentialRef) {
		profileRef := strings.TrimSpace(bedrockProfileFromCredentialRef(credentialRef)) // swobu:io-string source=boundary
		if profileRef == "" {
			return ModelSelectionGateState{
				Blocked: true,
				Reason:  ModelSelectionBlockBedrockProfileMissing,
				Message: "select AWS profile before loading models",
			}
		}
		if !bedrockAWSProfileAvailable(profileRef) {
			return ModelSelectionGateState{
				Blocked: true,
				Reason:  ModelSelectionBlockBedrockProfileUnavailable,
				Message: fmt.Sprintf("bedrock AWS profile %q is not available in the active AWS config", profileRef),
			}
		}
	}
	if profile.RequiresExplicitExecuteBaseURL(provider) && baseURL == "" { // swobu:io-string source=boundary
		return ModelSelectionGateState{
			Blocked: true,
			Reason:  ModelSelectionBlockGenericAuth,
			Message: "set backend URL before loading models",
		}
	}
	if strings.EqualFold(strings.TrimSpace(credentialSource(credentialRef)), "env") { // swobu:io-string source=boundary
		key := strings.TrimSpace(envCredentialKey(credentialRef)) // swobu:io-string source=boundary
		if key == "" {
			key = profile.DefaultEnvKeyForSpec(provider)
		}
		if strings.TrimSpace(os.Getenv(key)) == "" { // swobu:io-string source=boundary
			return ModelSelectionGateState{
				Blocked: true,
				Reason:  ModelSelectionBlockEnvVarMissing,
				Message: "set env var " + key + " before loading models",
			}
		}
	}
	if !ProviderModelCatalogLoadBlocked(provider, baseURL, credentialRef) {
		return ModelSelectionGateState{}
	}
	if message := strings.TrimSpace(ProviderModelCatalogBlockedMessage(provider, baseURL, credentialRef)); message != "" { // swobu:io-string source=boundary
		return ModelSelectionGateState{Blocked: true, Reason: ModelSelectionBlockGenericAuth, Message: message}
	}
	return ModelSelectionGateState{Blocked: true, Reason: ModelSelectionBlockGenericAuth, Message: "authenticate to load models"}
}

func bedrockProfileFromCredentialRef(ref string) string {
	ref = strings.TrimSpace(ref) // swobu:io-string source=boundary
	if !strings.HasPrefix(strings.ToLower(ref), "profile:") {
		return ""
	}
	return strings.TrimSpace(ref[len("profile:"):]) // swobu:io-string source=boundary
}

func isBedrockAWSProfileRef(ref string) bool {
	trimmed := strings.TrimSpace(ref) // swobu:io-string source=boundary
	if trimmed == "" {
		return true
	}
	if strings.EqualFold(trimmed, "aws_profile") {
		return true
	}
	if strings.EqualFold(trimmed, "aws_env_session") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(trimmed), "profile:") // swobu:io-string source=boundary
}

func envCredentialKey(ref string) string {
	if !strings.HasPrefix(strings.ToLower(ref), "env:") { // swobu:io-string source=boundary
		return ""
	}
	return strings.TrimSpace(ref[len("env:"):]) // swobu:io-string source=boundary
}
