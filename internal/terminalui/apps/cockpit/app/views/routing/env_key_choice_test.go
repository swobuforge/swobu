package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestApplyProviderEnvKeySelection_CreateModeLoadsCatalogScope(t *testing.T) {
	t.Parallel()

	actions := applyProviderEnvKeySelection("anthropic", "ANTHROPIC_API_KEY", nil, "", true, "")
	if len(actions) != 3 {
		t.Fatalf("action count = %d, want 3", len(actions))
	}
	load, ok := actions[2].(state.LoadRoutingModelCatalogRequestedAction)
	if !ok {
		t.Fatalf("action[2] = %T, want state.LoadRoutingModelCatalogRequestedAction", actions[2])
	}
	if load.Scope != state.RoutingModelCatalogScopeCreateDraft {
		t.Fatalf("scope = %q, want %q", load.Scope, state.RoutingModelCatalogScopeCreateDraft)
	}
}
