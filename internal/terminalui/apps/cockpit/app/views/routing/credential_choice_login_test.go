package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestApplyProviderCredentialSelection_ChatGPTLoginTriggersAuthSession(t *testing.T) {
	pc := &state.ProviderConfigSnapshot{Ref: "cfg-a", ProviderSpec: "chatgpt"}
	actions := applyProviderCredentialSelection(profile.AuthModeChatGPTLogin, "chatgpt", pc, "acme", false)
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
	if _, ok := actions[0].(state.StartProviderAuthSessionRequested); !ok {
		t.Fatalf("action type=%T want StartProviderAuthSessionRequested", actions[0])
	}
}

func TestApplyProviderCredentialSelection_ChatGPTLoginCreateModeSetsLoginMarker(t *testing.T) {
	t.Parallel()
	actions := applyProviderCredentialSelection(profile.AuthModeChatGPTLogin, "chatgpt", nil, "", true)
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

func TestCredentialOptionItems_Bedrock_DoesNotExposeProfileRefAsCredentialChoice(t *testing.T) {
	t.Parallel()
	items := credentialOptionItems("profile:swobu-bedrock", nil, "bedrock")
	if len(items) == 0 {
		t.Fatal("expected non-empty credential items")
	}
	for _, item := range items {
		if item.Label == "profile:swobu-bedrock" {
			t.Fatal("bedrock credential options must not expose serialized profile ref")
		}
	}
}

func TestCredentialOptionItems_Bedrock_IncludesCanonicalModesOnly(t *testing.T) {
	t.Parallel()
	items := credentialOptionItems("profile:swobu-bedrock", nil, "bedrock")
	labels := map[string]bool{}
	for _, item := range items {
		labels[item.Label] = true
	}
	if !labels["AWS chain"] {
		t.Fatalf("labels=%v; missing AWS chain", labels)
	}
	if !labels["Bedrock API key"] {
		t.Fatalf("labels=%v; missing Bedrock API key", labels)
	}
}

func TestCredentialOptionItems_OpenAIIncludesPasteRawSource(t *testing.T) {
	t.Parallel()
	items := credentialOptionItems("", nil, "openai")
	labels := map[string]bool{}
	for _, item := range items {
		labels[item.Label] = true
	}
	if !labels["paste raw"] {
		t.Fatalf("labels=%v; missing paste raw", labels)
	}
}

func TestCredentialOptionItems_ExposeCanonicalModeToSelectionCallback(t *testing.T) {
	t.Parallel()
	var got credentialChoiceOption
	items := credentialOptionItems("", func(choice credentialChoiceOption) []update.Action {
		got = choice
		return nil
	}, "openai")
	var found bool
	for _, item := range items {
		if item.Label == "paste raw" {
			found = true
			item.OnChoose()
			break
		}
	}
	if !found {
		t.Fatal("missing paste raw option")
	}
	if got.Mode != profile.AuthModeKeychain {
		t.Fatalf("choice mode=%q want keychain", got.Mode)
	}
	if got.FocusKey != "keychain" {
		t.Fatalf("choice focus key=%q want keychain", got.FocusKey)
	}
}
