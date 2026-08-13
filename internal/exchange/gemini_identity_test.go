package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestProviderTargetFromGeminiStandardConnectionPreservesInteractionsIdentity(t *testing.T) {
	providerID, err := routing.ParseProvider("gemini", profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := routing.NewStandardConnection(providerID, "", "env:GEMINI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	target, err := ProviderTargetFromConnection("gemini-target", connection, "interactions_stream")
	if err != nil {
		t.Fatal(err)
	}
	if target.ProviderSpec != "gemini" || target.BaseURL != "https://generativelanguage.googleapis.com/v1" || target.CredentialRef != "env:GEMINI_API_KEY" {
		t.Fatalf("Gemini target = %#v", target)
	}
	if target.ProtocolKind != protocolkind.Interactions || target.SelectedFrame != profile.FrameSSEEvent || target.ProviderProtocol != "interactions_stream" {
		t.Fatalf("Gemini execution identity = %#v", target)
	}
}
