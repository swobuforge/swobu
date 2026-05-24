package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestCreateDraftModelBinding_LoadCatalogUsesCreateAction(t *testing.T) {
	t.Parallel()

	binding := createDraftModelBinding{}
	actions := binding.LoadCatalog(state.ProviderConfigSnapshot{
		ProviderSpec:  "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		CredentialRef: "file:/tmp/openrouter.key",
	})
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
	load, ok := actions[0].(state.LoadRoutingModelCatalogRequestedAction)
	if !ok {
		t.Fatalf("action type=%T want state.LoadRoutingModelCatalogRequestedAction", actions[0])
	}
	if load.Scope != state.RoutingModelCatalogScopeCreateDraft {
		t.Fatalf("scope=%q want %q", load.Scope, state.RoutingModelCatalogScopeCreateDraft)
	}
	if load.ProviderSpec != "openrouter" || load.CredentialRef != "file:/tmp/openrouter.key" {
		t.Fatalf("unexpected load action: %+v", load)
	}
}

func TestCreateDraftModelBinding_LoadCatalogDerivesBedrockBaseURLFromRegion(t *testing.T) {
	t.Parallel()

	binding := createDraftModelBinding{}
	actions := binding.LoadCatalog(state.ProviderConfigSnapshot{
		ProviderSpec:  "bedrock",
		Region:        "eu-west-2",
		CredentialRef: "profile:swobu-bedrock",
	})
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
	load, ok := actions[0].(state.LoadRoutingModelCatalogRequestedAction)
	if !ok {
		t.Fatalf("action type=%T want state.LoadRoutingModelCatalogRequestedAction", actions[0])
	}
	wantBaseURL := "https://bedrock-runtime.eu-west-2.amazonaws.com/openai/v1"
	if load.BaseURL != wantBaseURL {
		t.Fatalf("base URL=%q want %q", load.BaseURL, wantBaseURL)
	}
}

func TestAddDraftModelBinding_LoadCatalogUsesAddDraftAction(t *testing.T) {
	t.Parallel()

	binding := addDraftModelBinding{}
	actions := binding.LoadCatalog(state.ProviderConfigSnapshot{
		ProviderSpec:  "openrouter",
		BaseURL:       "https://openrouter.ai/api/v1",
		CredentialRef: "file:/tmp/openrouter.key",
	})
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
	load, ok := actions[0].(state.LoadRoutingModelCatalogRequestedAction)
	if !ok {
		t.Fatalf("action type=%T want state.LoadRoutingModelCatalogRequestedAction", actions[0])
	}
	if load.Scope != state.RoutingModelCatalogScopeAddModelDraft {
		t.Fatalf("scope=%q want %q", load.Scope, state.RoutingModelCatalogScopeAddModelDraft)
	}
	if load.ProviderSpec != "openrouter" || load.CredentialRef != "file:/tmp/openrouter.key" {
		t.Fatalf("unexpected load action: %+v", load)
	}
}

func TestAddDraftModelBinding_SetSnapshotMutatesDraftAuthority(t *testing.T) {
	t.Parallel()

	got := state.ProviderConfigSnapshot{}
	binding := addDraftModelBinding{
		setDraft: func(next state.ProviderConfigSnapshot) { got = next },
	}
	binding.SetSnapshot(state.ProviderConfigSnapshot{ModelID: "openai/gpt-4.1-mini"})
	if got.ModelID != "openai/gpt-4.1-mini" {
		t.Fatalf("model id=%q want openai/gpt-4.1-mini", got.ModelID)
	}
}

func TestDraftModelBindings_ExposeDistinctCloseModes(t *testing.T) {
	t.Parallel()

	if got := (createDraftModelBinding{}).CloseMode(); got != state.InteractionModeNAV {
		t.Fatalf("create close mode=%q want %q", got, state.InteractionModeNAV)
	}
	if got := (addDraftModelBinding{}).CloseMode(); got != state.InteractionModeManageList {
		t.Fatalf("add close mode=%q want %q", got, state.InteractionModeManageList)
	}
}

func TestProviderModelCatalogLoadBlocked_FileCredentialGate(t *testing.T) {
	t.Parallel()

	if !state.ProviderModelCatalogLoadBlocked("openrouter", "", "file") {
		t.Fatalf("expected unresolved file credential to block model catalog load")
	}
	if state.ProviderModelCatalogLoadBlocked("openrouter", "", "file:/tmp/openrouter.key") {
		t.Fatalf("expected resolved file credential to allow model catalog load")
	}
}

func TestProviderModelCatalogLoadBlocked_ChatGPTLoginGate(t *testing.T) {
	t.Parallel()

	if !state.ProviderModelCatalogLoadBlocked("chatgpt", "", "") {
		t.Fatalf("expected missing chatgpt login to block model catalog load")
	}
	if !state.ProviderModelCatalogLoadBlocked("chatgpt", "", "chatgpt_login") {
		t.Fatalf("expected pre-login marker to block model catalog load")
	}
	if state.ProviderModelCatalogLoadBlocked("chatgpt", "", "keychain:chatgpt/default") {
		t.Fatalf("expected resolved chatgpt credential ref to allow model catalog load")
	}
	if got := state.ProviderModelCatalogBlockedMessage("chatgpt", "", ""); got != "" {
		t.Fatalf("blocked message=%q", got)
	}
}

func TestProviderModelCatalogAuthFailed_RecognizesCredentialAndUnauthorizedErrors(t *testing.T) {
	t.Parallel()

	if !state.ProviderModelCatalogAuthFailed("BAD_ENDPOINT: bedrock API key env var is missing: AWS_BEARER_TOKEN_BEDROCK") {
		t.Fatal("expected credential error to be classified as auth failure")
	}
	if !state.ProviderModelCatalogAuthFailed("401 Unauthorized") {
		t.Fatal("expected unauthorized error to be classified as auth failure")
	}
	if state.ProviderModelCatalogAuthFailed("model catalog request timed out") {
		t.Fatal("did not expect timeout error to be classified as auth failure")
	}
}

func TestProviderModelCatalogAuthFailureMessage_OnlyPresentForAuthErrors(t *testing.T) {
	t.Parallel()

	errText := "BAD_ENDPOINT: credential reference could not be resolved"
	if got := state.ProviderModelCatalogAuthFailureMessage(errText); got != errText {
		t.Fatalf("auth failure message=%q want passthrough error %q", got, errText)
	}
	if got := state.ProviderModelCatalogAuthFailureMessage("request timed out"); got != "" {
		t.Fatalf("auth failure message=%q want empty for non-auth errors", got)
	}
}

func TestEvaluateModelCatalogReadiness_AuthFailureUsesRawProbeError(t *testing.T) {
	t.Parallel()

	errText := "BAD_ENDPOINT: bedrock API key env var is missing: AWS_BEARER_TOKEN_BEDROCK"
	got := state.EvaluateModelSelectionGateState(state.ModelSelectionReadinessGateInput{
		ProviderSpec:      "bedrock",
		BaseURL:           "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1",
		CredentialRef:     "env:AWS_BEARER_TOKEN_BEDROCK",
		ModelCatalogError: errText,
	})
	if !got.Blocked {
		t.Fatal("expected blocked message for auth failure")
	}
	if got.Message != errText {
		t.Fatalf("blocked message=%q want raw probe error %q", got.Message, errText)
	}
}
