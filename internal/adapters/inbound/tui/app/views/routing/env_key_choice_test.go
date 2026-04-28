package routing

import (
	"testing"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state"
)

func TestApplyProviderEnvKeySelection_CreateModeUsesProviderDefaultProtocol(t *testing.T) {
	t.Parallel()

	actions := applyProviderEnvKeySelection("anthropic", "ANTHROPIC_API_KEY", nil, "", true, "")
	if len(actions) != 3 {
		t.Fatalf("action count = %d, want 3", len(actions))
	}
	load, ok := actions[2].(state.LoadCreateDraftModelCatalogRequested)
	if !ok {
		t.Fatalf("action[2] = %T, want state.LoadCreateDraftModelCatalogRequested", actions[2])
	}
	if got := load.ProtocolKind; got != "messages" {
		t.Fatalf("protocol kind = %q, want %q", got, "messages")
	}
}
