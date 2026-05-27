package state

import (
	"testing"

	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
)

func TestReduce_ProbeOrchestration_CreateDraftCatalogAndProbeFlow(t *testing.T) {
	t.Parallel()

	model := Model{
		CreateDraftProviderConfig: ProviderConfigSnapshot{
			ProviderSpec:     "openai",
			BaseURL:          "https://api.openai.com/v1",
			CredentialRef:    "env:OPENAI_API_KEY",
			ProviderProtocol: "auto",
			ModelID:          "gpt-5.4-mini",
		},
	}

	catalogEffects := Reduce(&model, LoadRoutingModelCatalogRequestedAction{
		Scope:            RoutingModelCatalogScopeCreateDraft,
		ProviderSpec:     "openai",
		BaseURL:          "https://api.openai.com/v1",
		CredentialRef:    "env:OPENAI_API_KEY",
		ProviderProtocol: "auto",
	})
	if len(catalogEffects) != 1 {
		t.Fatalf("catalog effects len=%d want 1", len(catalogEffects))
	}
	loadEff, ok := catalogEffects[0].(stateeffect.LoadRoutingModelCatalogEffect)
	if !ok {
		t.Fatalf("catalog effect type=%T want LoadRoutingModelCatalogEffect", catalogEffects[0])
	}
	if !model.CreateDraftModelProbePending {
		t.Fatal("create draft probe pending=false want true")
	}

	Reduce(&model, stateeffect.RoutingModelCatalogLoaded{
		Scope:                    loadEff.Scope,
		ProviderSpec:             loadEff.ProviderSpec,
		BaseURL:                  loadEff.BaseURL,
		CredentialRef:            loadEff.CredentialRef,
		ProviderProtocol:         loadEff.ProviderProtocol,
		ModelIDs:                 []string{"gpt-5.4-mini", "gpt-5.5"},
		ResolvedProviderProtocol: "responses_stream",
	})
	if model.CreateDraftModelProbePending {
		t.Fatal("create draft probe pending=true want false")
	}
	if len(model.CreateDraftModelIDs) != 2 {
		t.Fatalf("create draft model ids=%v", model.CreateDraftModelIDs)
	}
	if got := model.CreateDraftProviderConfig.ProviderProtocol; got != "auto" {
		t.Fatalf("provider protocol=%q want auto", got)
	}
	if !model.CreateDraftModelTestPassed {
		t.Fatal("create draft test passed=false want true after successful catalog load")
	}

}

func TestReduce_ProbeOrchestration_AddModelCatalogStaleResultIgnoredThenAccepted(t *testing.T) {
	t.Parallel()

	model := Model{}

	effects := Reduce(&model, LoadRoutingModelCatalogRequestedAction{
		Scope:            RoutingModelCatalogScopeAddModelDraft,
		ProviderSpec:     "ollama",
		BaseURL:          "http://127.0.0.1:11434/v1",
		CredentialRef:    "",
		ProviderProtocol: "responses_stream",
	})
	if len(effects) != 1 {
		t.Fatalf("effects len=%d want 1", len(effects))
	}
	loadEff, ok := effects[0].(stateeffect.LoadRoutingModelCatalogEffect)
	if !ok {
		t.Fatalf("effects[0]=%T want LoadRoutingModelCatalogEffect", effects[0])
	}
	if !model.AddModelDraftModelProbePending {
		t.Fatal("add-model probe pending=false want true")
	}

	// Stale completion must be ignored because provider/baseURL context no longer matches.
	Reduce(&model, stateeffect.RoutingModelCatalogLoaded{
		Scope:            loadEff.Scope,
		ProviderSpec:     "openai",
		BaseURL:          "https://api.openai.com/v1",
		CredentialRef:    "env:OPENAI_API_KEY",
		ProviderProtocol: loadEff.ProviderProtocol,
		ModelIDs:         []string{"should-not-apply"},
	})
	if len(model.AddModelDraftModelIDs) != 0 {
		t.Fatalf("stale completion applied model ids=%v", model.AddModelDraftModelIDs)
	}
	if !model.AddModelDraftModelProbePending {
		t.Fatal("pending should remain true after stale completion")
	}

	Reduce(&model, stateeffect.RoutingModelCatalogLoaded{
		Scope:            loadEff.Scope,
		ProviderSpec:     loadEff.ProviderSpec,
		BaseURL:          loadEff.BaseURL,
		CredentialRef:    loadEff.CredentialRef,
		ProviderProtocol: loadEff.ProviderProtocol,
		ModelIDs:         []string{"llama3.1", "gemma3:4b"},
	})
	if model.AddModelDraftModelProbePending {
		t.Fatal("pending should clear after matching completion")
	}
	if len(model.AddModelDraftModelIDs) != 2 {
		t.Fatalf("add-model ids=%v", model.AddModelDraftModelIDs)
	}
}
