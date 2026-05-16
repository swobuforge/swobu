package effect

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
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

func TestArgsToProviderConfig_PreservesSelectedFrame(t *testing.T) {
	t.Parallel()

	cfg, err := argsToProviderConfig(stateModel.ProviderConfigSnapshot{
		Ref:           "backend-a",
		ProviderSpec:  "openai",
		SelectedFrame: providercatalog.FrameSSEEvent,
		ModelID:       "gpt-5.4-mini",
		CredentialRef: "env:OPENAI_API_KEY",
	})
	if err != nil {
		t.Fatalf("argsToProviderConfig returned error: %v", err)
	}
	if got := cfg.SelectedFrame(); got != providercatalog.FrameSSEEvent {
		t.Fatalf("selected frame=%q want=%q", got, providercatalog.FrameSSEEvent)
	}
}
