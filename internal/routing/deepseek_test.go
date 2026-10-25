package routing

import "testing"

func TestTargetReferencesRequireExplicitProtocolForEveryProvider(t *testing.T) {
	for _, protocol := range []string{DeepSeekProviderProtocol, KimiProviderProtocol, "chat_completions"} {
		target, err := FinalizeTarget(TargetDraft{ID: "target", Model: "synthetic", Protocol: protocol, ConnectionID: "cn_shared"}, TargetConstructionFacts{})
		if err != nil || target.Protocol().String() != protocol || target.ConnectionID().String() != "cn_shared" {
			t.Fatal("explicit reference protocol lost", err)
		}
	}
	if _, err := FinalizeTarget(TargetDraft{ID: "target", Model: "synthetic", ConnectionID: "cn_shared"}, TargetConstructionFacts{}); err == nil {
		t.Fatal("missing protocol admitted")
	}
}
