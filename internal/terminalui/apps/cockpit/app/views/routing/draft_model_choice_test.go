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
	load, ok := actions[0].(state.LoadRoutingModelCatalogRequested)
	if !ok {
		t.Fatalf("action type=%T want state.LoadRoutingModelCatalogRequested", actions[0])
	}
	if load.Scope != state.RoutingModelCatalogScopeCreateDraft {
		t.Fatalf("scope=%q want %q", load.Scope, state.RoutingModelCatalogScopeCreateDraft)
	}
	if load.ProviderSpec != "openrouter" || load.CredentialRef != "file:/tmp/openrouter.key" {
		t.Fatalf("unexpected load action: %+v", load)
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
	load, ok := actions[0].(state.LoadRoutingModelCatalogRequested)
	if !ok {
		t.Fatalf("action type=%T want state.LoadRoutingModelCatalogRequested", actions[0])
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

	if !providerModelCatalogLoadBlocked("openrouter", "", "file") {
		t.Fatalf("expected unresolved file credential to block model catalog load")
	}
	if providerModelCatalogLoadBlocked("openrouter", "", "file:/tmp/openrouter.key") {
		t.Fatalf("expected resolved file credential to allow model catalog load")
	}
}
