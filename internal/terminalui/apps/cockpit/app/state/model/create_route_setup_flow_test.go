package model

import "testing"

func TestEvaluateCreateDraftRouteSetup_BedrockMissingScopeBlocksModel(t *testing.T) {
	got := EvaluateCreateDraftRouteSetup(ProviderConfigSnapshot{
		ProviderSpec:  "bedrock",
		CredentialRef: "",
		BaseURL:       "",
		ModelID:       "",
	})
	if got.Credential != RouteSetupSlotMissing {
		t.Fatalf("credential state = %q, want %q", got.Credential, RouteSetupSlotMissing)
	}
	if !got.ScopeVisible {
		t.Fatalf("scope visible = false, want true")
	}
	if got.ScopeState != RouteSetupSlotMissing {
		t.Fatalf("scope state = %q, want %q", got.ScopeState, RouteSetupSlotMissing)
	}
	if got.ModelState != RouteSetupSlotBlocked {
		t.Fatalf("model state = %q, want %q", got.ModelState, RouteSetupSlotBlocked)
	}
	if got.ModelBlocker == "" {
		t.Fatalf("model blocker empty, want explicit blocker message")
	}
	if got.ModelBlocker != "set credential before loading models" {
		t.Fatalf("model blocker = %q, want credential blocker first", got.ModelBlocker)
	}
	if got.Ready {
		t.Fatalf("ready = true, want false")
	}
}

func TestEvaluateCreateDraftRouteSetup_BedrockReadyWhenScopeAndModelSet(t *testing.T) {
	got := EvaluateCreateDraftRouteSetup(ProviderConfigSnapshot{
		ProviderSpec:  "bedrock",
		CredentialRef: "profile:default",
		BaseURL:       "https://bedrock-runtime.eu-west-2.amazonaws.com/openai/v1",
		ModelID:       "anthropic.claude-3-5-sonnet-20241022-v2:0",
	})
	if got.ScopeState != RouteSetupSlotReady {
		t.Fatalf("scope state = %q, want %q", got.ScopeState, RouteSetupSlotReady)
	}
	if got.ModelState != RouteSetupSlotReady {
		t.Fatalf("model state = %q, want %q", got.ModelState, RouteSetupSlotReady)
	}
	if !got.Ready {
		t.Fatalf("ready = false, want true")
	}
}

func TestEvaluateCreateDraftRouteSetup_BedrockExplicitAWSProfileIsExternal(t *testing.T) {
	got := EvaluateCreateDraftRouteSetup(ProviderConfigSnapshot{
		ProviderSpec:  "bedrock",
		CredentialRef: "aws_profile",
		BaseURL:       "https://bedrock-runtime.eu-west-2.amazonaws.com/openai/v1",
		ModelID:       "anthropic.claude-3-5-sonnet-20241022-v2:0",
	})
	if got.Credential != RouteSetupSlotExternal {
		t.Fatalf("credential state = %q, want %q", got.Credential, RouteSetupSlotExternal)
	}
	if !got.Ready {
		t.Fatalf("ready = false, want true")
	}
}

func TestCreateDraftCredentialStrategySelectable_ProviderDeclaredVariants(t *testing.T) {
	if CreateDraftCredentialStrategySelectable("ollama") {
		t.Fatalf("ollama should not expose credential strategy chooser")
	}
	if !CreateDraftCredentialStrategySelectable("bedrock") {
		t.Fatalf("bedrock should expose credential strategy chooser")
	}
	if !CreateDraftCredentialStrategySelectable("openai") {
		t.Fatalf("openai should expose credential strategy chooser")
	}
}

func TestEvaluateCreateDraftRouteSetup_BedrockMissingCredentialBlocksBeforeScope(t *testing.T) {
	got := EvaluateCreateDraftRouteSetup(ProviderConfigSnapshot{
		ProviderSpec: "bedrock",
		BaseURL:      "",
		ModelID:      "",
	})
	if got.ModelState != RouteSetupSlotBlocked {
		t.Fatalf("model state = %q, want %q", got.ModelState, RouteSetupSlotBlocked)
	}
	if got.ModelBlocker != "set credential before loading models" {
		t.Fatalf("model blocker = %q, want credential-first blocker", got.ModelBlocker)
	}
}

func TestEvaluateCreateDraftRouteSetup_BedrockMissingRegionUsesRegionBlocker(t *testing.T) {
	got := EvaluateCreateDraftRouteSetup(ProviderConfigSnapshot{
		ProviderSpec:  "bedrock",
		CredentialRef: "profile:default",
		BaseURL:       "",
		ModelID:       "",
	})
	if got.ModelState != RouteSetupSlotBlocked {
		t.Fatalf("model state = %q, want %q", got.ModelState, RouteSetupSlotBlocked)
	}
	if got.ModelBlocker != "choose region before loading models" {
		t.Fatalf("model blocker = %q, want region blocker", got.ModelBlocker)
	}
}

func TestEvaluateCreateDraftRouteSetup_BedrockRegionSetWithoutBaseURL_DoesNotUseRegionBlocker(t *testing.T) {
	got := EvaluateCreateDraftRouteSetup(ProviderConfigSnapshot{
		ProviderSpec:  "bedrock",
		CredentialRef: "profile:default",
		Region:        "eu-west-2",
		BaseURL:       "",
		ModelID:       "",
	})
	if got.ScopeState != RouteSetupSlotReady {
		t.Fatalf("scope state = %q, want %q", got.ScopeState, RouteSetupSlotReady)
	}
	if got.ModelState != RouteSetupSlotMissing {
		t.Fatalf("model state = %q, want %q", got.ModelState, RouteSetupSlotMissing)
	}
	if got.ModelBlocker != "" {
		t.Fatalf("model blocker = %q, want empty blocker when only model is missing", got.ModelBlocker)
	}
}

func TestEvaluateCreateDraftRouteSetup_BedrockEnvRegionWithoutBaseURL_DoesNotUseRegionBlocker(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("AWS_DEFAULT_REGION", "")
	got := EvaluateCreateDraftRouteSetup(ProviderConfigSnapshot{
		ProviderSpec:  "bedrock",
		CredentialRef: "profile:default",
		Region:        "",
		BaseURL:       "",
		ModelID:       "",
	})
	if got.ScopeState != RouteSetupSlotReady {
		t.Fatalf("scope state = %q, want %q", got.ScopeState, RouteSetupSlotReady)
	}
	if got.ModelState != RouteSetupSlotMissing {
		t.Fatalf("model state = %q, want %q", got.ModelState, RouteSetupSlotMissing)
	}
	if got.ModelBlocker != "" {
		t.Fatalf("model blocker = %q, want empty blocker when only model is missing", got.ModelBlocker)
	}
}

func TestEvaluateCreateDraftRouteSetup_OllamaHidesCredentialSlot(t *testing.T) {
	got := EvaluateCreateDraftRouteSetup(ProviderConfigSnapshot{
		ProviderSpec: "ollama",
		BaseURL:      "http://127.0.0.1:11434/v1",
	})
	if got.CredentialVisible {
		t.Fatalf("credential visible = true, want false")
	}
	if got.Credential != RouteSetupSlotExternal {
		t.Fatalf("credential state = %q, want %q", got.Credential, RouteSetupSlotExternal)
	}
}
