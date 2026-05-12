package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestApplyProviderCredentialSelection_ChatGPTLoginTriggersAuthSession(t *testing.T) {
	pc := &state.ProviderConfigSnapshot{Ref: "cfg-a", ProviderSpec: "chatgpt"}
	actions := applyProviderCredentialSelection("chatgpt_login", "chatgpt", pc, "acme", false)
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
	if _, ok := actions[0].(state.StartProviderAuthSessionRequested); !ok {
		t.Fatalf("action type=%T want StartProviderAuthSessionRequested", actions[0])
	}
}

func TestApplyProviderCredentialSelection_ChatGPTLoginCreateModeSetsLoginMarker(t *testing.T) {
	t.Parallel()
	actions := applyProviderCredentialSelection("chatgpt_login", "chatgpt", nil, "", true)
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
	set, ok := actions[0].(state.SetCreateDraftCredentialRef)
	if !ok {
		t.Fatalf("action type=%T want SetCreateDraftCredentialRef", actions[0])
	}
	if set.CredentialRef != "chatgpt_login" {
		t.Fatalf("credential ref=%q want chatgpt_login", set.CredentialRef)
	}
}

func TestCredentialOptionRows_ChatGPTShowsLoginAndTokenSources(t *testing.T) {
	t.Parallel()
	rows := credentialOptionRows("", nil, nil, "chatgpt", false)
	if len(rows) != 2 {
		t.Fatalf("rows len=%d want 2", len(rows))
	}
}
