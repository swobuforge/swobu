package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func TestApplyAddModelCredentialSourceChoice_ChatGPTLoginMarker(t *testing.T) {
	t.Parallel()
	draft := state.ProviderConfigSnapshot{ProviderSpec: "chatgpt", CredentialRef: "", ModelID: "openai/gpt-4.1-mini"}
	next := applyAddModelCredentialSourceChoice(draft, "chatgpt_login")
	if next.CredentialRef != "chatgpt_login" {
		t.Fatalf("credential ref=%q want chatgpt_login", next.CredentialRef)
	}
	if next.ModelID != "" {
		t.Fatalf("model id=%q want cleared model id", next.ModelID)
	}
}
