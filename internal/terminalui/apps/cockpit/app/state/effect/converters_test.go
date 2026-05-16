package effect

import (
	"testing"

	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func TestArgsToProviderConfig_IgnoresLegacyProtocolTupleInput(t *testing.T) {
	t.Parallel()

	_, err := argsToProviderConfig(stateModel.ProviderConfigSnapshot{
		Ref:           "backend-a",
		ProviderSpec:  "anthropic",
		ProtocolKind:  "chat_completions",
		ModelID:       "claude-sonnet",
		CredentialRef: "env:ANTHROPIC_API_KEY",
	})
	if err != nil {
		t.Fatalf("argsToProviderConfig returned error: %v", err)
	}
}
