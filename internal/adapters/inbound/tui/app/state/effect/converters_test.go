package effect

import (
	"strings"
	"testing"

	stateModel "github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state/model"
)

func TestArgsToProviderConfig_RejectsUnsupportedProviderProtocolTuple(t *testing.T) {
	t.Parallel()

	_, err := argsToProviderConfig(stateModel.ProviderConfigSnapshot{
		Ref:           "backend-a",
		ProviderSpec:  "anthropic",
		ProtocolKind:  "chat_completions",
		ModelID:       "claude-sonnet",
		CredentialRef: "env:ANTHROPIC_API_KEY",
	})
	if err == nil {
		t.Fatal("argsToProviderConfig returned nil error, want validation failure")
	}
	if !strings.Contains(err.Error(), `unsupported provider route "anthropic" + "chat_completions"`) {
		t.Fatalf("error = %q, want unsupported provider route message", err.Error())
	}
}
