package routing

import (
	"strings"
	"testing"
)

func TestDeepSeekConnectionDerivesMessagesStream(t *testing.T) {
	connection, err := NewAPIKeyConnection(ProviderDeepSeek, "env:DEEPSEEK_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	target, err := FinalizeTarget(TargetDraft{
		ID:         "deepseek-pro",
		Model:      "deepseek-v4-pro",
		Connection: ConnectionDraft{APIKey: &APIKeyConnectionDraft{Provider: ProviderDeepSeek, Credential: "env:DEEPSEEK_API_KEY"}},
	}, TargetConstructionFacts{
		ProtocolSupported: func(provider Provider, protocol string) bool {
			return provider == ProviderDeepSeek && protocol == DeepSeekProviderProtocol
		},
		DerivedProtocol: func(provider Provider) (string, bool) {
			return DeepSeekProviderProtocol, provider == ProviderDeepSeek
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider() != ProviderDeepSeek || target.Protocol().String() != DeepSeekProviderProtocol {
		t.Fatalf("target provider/protocol = %q/%q", target.Provider(), target.Protocol().String())
	}
	if !connectionsEqual(connection, target.Connection()) {
		t.Fatal("DeepSeek connection equality did not preserve credential reference")
	}
}

func TestDeepSeekRejectsAuthoredProtocol(t *testing.T) {
	_, err := FinalizeTarget(TargetDraft{
		ID:         "deepseek-pro",
		Model:      "deepseek-v4-pro",
		Protocol:   DeepSeekProviderProtocol,
		Connection: ConnectionDraft{APIKey: &APIKeyConnectionDraft{Provider: ProviderDeepSeek, Credential: "env:DEEPSEEK_API_KEY"}},
	}, TargetConstructionFacts{
		ProtocolSupported: func(provider Provider, protocol string) bool { return true },
		DerivedProtocol: func(provider Provider) (string, bool) {
			return DeepSeekProviderProtocol, provider == ProviderDeepSeek
		},
	})
	if err == nil || !strings.Contains(err.Error(), "provider protocol is derived and must be omitted") {
		t.Fatalf("FinalizeTarget error = %v", err)
	}
}
