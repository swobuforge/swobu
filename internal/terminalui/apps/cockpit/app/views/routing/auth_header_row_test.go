package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestApplyProviderAuthHeaderSelection_CreateModeReloadsCatalog(t *testing.T) {
	t.Parallel()

	draft := state.ProviderConfigSnapshot{
		ProviderSpec:     "openai_compatible",
		BaseURL:          "https://example.test/v1",
		CredentialRef:    "env:OPENAI_API_KEY",
		ProviderProtocol: "responses_stream",
	}
	actions := applyProviderAuthHeaderSelection("X-API-Key", &draft, "", true)
	if len(actions) != 3 {
		t.Fatalf("actions len=%d want 3", len(actions))
	}
	if _, ok := actions[0].(state.SetCreateDraftAuthHeaderAction); !ok {
		t.Fatalf("actions[0]=%T want SetCreateDraftAuthHeaderAction", actions[0])
	}
	if _, ok := actions[1].(state.SetCreateDraftModelIDAction); !ok {
		t.Fatalf("actions[1]=%T want SetCreateDraftModelIDAction", actions[1])
	}
	load, ok := actions[2].(state.LoadRoutingModelCatalogRequestedAction)
	if !ok {
		t.Fatalf("actions[2]=%T want LoadRoutingModelCatalogRequestedAction", actions[2])
	}
	if load.Scope != state.RoutingModelCatalogScopeCreateDraft {
		t.Fatalf("scope=%q want=%q", load.Scope, state.RoutingModelCatalogScopeCreateDraft)
	}
	if load.AuthHeader != "X-API-Key" {
		t.Fatalf("auth header=%q want X-API-Key", load.AuthHeader)
	}
	if load.ProviderSpec != "openai_compatible" {
		t.Fatalf("provider spec=%q want openai_compatible", load.ProviderSpec)
	}
}

func TestApplyAddModelAuthHeaderSelection_UpdatesDraftAndReloadsCatalog(t *testing.T) {
	t.Parallel()

	draft := state.ProviderConfigSnapshot{
		Ref:              "cfg-a",
		ProviderSpec:     "openai_compatible",
		BaseURL:          "https://example.test/v1",
		CredentialRef:    "env:OPENAI_API_KEY",
		ProviderProtocol: "responses_stream",
	}
	var saved state.ProviderConfigSnapshot
	panel := addModelPanelState{
		setDraft: func(next state.ProviderConfigSnapshot) {
			saved = next
		},
		setModelPickerOpen: func(bool) {},
	}

	actions := applyAddModelAuthHeaderSelection(state.Model{}, "openai_compatible", "X-API-Key", draft, panel)
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
	load, ok := actions[0].(state.LoadRoutingModelCatalogRequestedAction)
	if !ok {
		t.Fatalf("actions[0]=%T want LoadRoutingModelCatalogRequestedAction", actions[0])
	}
	if saved.AuthHeader != "X-API-Key" {
		t.Fatalf("saved auth header=%q want X-API-Key", saved.AuthHeader)
	}
	if saved.ModelID != "" {
		t.Fatalf("saved model id=%q want cleared", saved.ModelID)
	}
	if load.Scope != state.RoutingModelCatalogScopeAddModelDraft {
		t.Fatalf("scope=%q want=%q", load.Scope, state.RoutingModelCatalogScopeAddModelDraft)
	}
	if load.AuthHeader != "X-API-Key" {
		t.Fatalf("auth header=%q want X-API-Key", load.AuthHeader)
	}
	if load.CredentialRef != "env:OPENAI_API_KEY" {
		t.Fatalf("credential ref=%q want env:OPENAI_API_KEY", load.CredentialRef)
	}
}
