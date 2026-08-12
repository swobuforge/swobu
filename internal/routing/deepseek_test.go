package routing

import (
	"strings"
	"testing"
)

func preserveStandardDraft(_ Provider, draft StandardConnectionDraft) (StandardConnectionDraft, error) {
	return draft, nil
}

func TestDeepSeekConnectionDerivesMessagesStream(t *testing.T) {
	deepseek := supportedProvider("deepseek")
	connection, err := NewStandardConnection(deepseek, "", "env:DEEPSEEK_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	target, err := FinalizeTarget(TargetDraft{
		ID:         "deepseek-pro",
		Model:      "deepseek-v4-pro",
		Connection: ConnectionDraft{Provider: "deepseek", Standard: &StandardConnectionDraft{Credential: "env:DEEPSEEK_API_KEY"}},
	}, TargetConstructionFacts{
		ProviderSupported:          func(raw string) bool { return raw == "deepseek" },
		ConnectionShape:            func(Provider) (ConnectionShape, bool) { return ConnectionShapeStandard, true },
		ValidateStandardConnection: preserveStandardDraft,
		ProtocolSupported: func(provider Provider, protocol string) bool {
			return provider == deepseek && protocol == DeepSeekProviderProtocol
		},
		DerivedProtocol: func(provider Provider) (string, bool) {
			return DeepSeekProviderProtocol, provider == deepseek
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider() != deepseek || target.Protocol().String() != DeepSeekProviderProtocol {
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
		Connection: ConnectionDraft{Provider: "deepseek", Standard: &StandardConnectionDraft{Credential: "env:DEEPSEEK_API_KEY"}},
	}, TargetConstructionFacts{
		ProviderSupported:          func(raw string) bool { return raw == "deepseek" },
		ConnectionShape:            func(Provider) (ConnectionShape, bool) { return ConnectionShapeStandard, true },
		ValidateStandardConnection: preserveStandardDraft,
		ProtocolSupported:          func(provider Provider, protocol string) bool { return true },
		DerivedProtocol: func(provider Provider) (string, bool) {
			return DeepSeekProviderProtocol, provider == Provider("deepseek")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "provider protocol is derived and must be omitted") {
		t.Fatalf("FinalizeTarget error = %v", err)
	}
}

func TestKimiConnectionDerivesChatCompletionsStream(t *testing.T) {
	kimi := supportedProvider("kimi")
	target, err := FinalizeTarget(TargetDraft{
		ID: "kimi-k3", Model: "kimi-k3",
		Connection: ConnectionDraft{Provider: "kimi", Standard: &StandardConnectionDraft{Credential: "env:MOONSHOT_API_KEY"}},
	}, TargetConstructionFacts{
		ProviderSupported:          func(raw string) bool { return raw == "kimi" },
		ConnectionShape:            func(Provider) (ConnectionShape, bool) { return ConnectionShapeStandard, true },
		ValidateStandardConnection: preserveStandardDraft,
		ProtocolSupported: func(provider Provider, protocol string) bool {
			return provider == kimi && protocol == KimiProviderProtocol
		},
		DerivedProtocol: func(provider Provider) (string, bool) { return KimiProviderProtocol, provider == kimi },
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider() != kimi || target.Protocol().String() != KimiProviderProtocol {
		t.Fatalf("target provider/protocol = %q/%q", target.Provider(), target.Protocol().String())
	}
}

func TestDeepInfraConnectionDerivesChatCompletionsStream(t *testing.T) {
	deepinfra := supportedProvider("deepinfra")
	target, err := FinalizeTarget(TargetDraft{
		ID: "deepinfra-private", Model: "deploy_id:private",
		Connection: ConnectionDraft{Provider: "deepinfra", Standard: &StandardConnectionDraft{Credential: "env:DEEPINFRA_TOKEN"}},
	}, TargetConstructionFacts{
		ProviderSupported:          func(raw string) bool { return raw == "deepinfra" },
		ConnectionShape:            func(Provider) (ConnectionShape, bool) { return ConnectionShapeStandard, true },
		ValidateStandardConnection: preserveStandardDraft,
		ProtocolSupported: func(provider Provider, protocol string) bool {
			return provider == deepinfra && protocol == "chat_completions_stream"
		},
		DerivedProtocol: func(provider Provider) (string, bool) {
			return "chat_completions_stream", provider == deepinfra
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider() != deepinfra || target.Protocol().String() != "chat_completions_stream" || target.Model().String() != "deploy_id:private" {
		t.Fatalf("target = %#v", target)
	}
}
