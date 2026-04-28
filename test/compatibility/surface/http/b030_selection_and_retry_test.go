package http_test

import (
	"testing"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/routetarget"
)

func TestB030_SelectedTargetResolutionIsExplicitAndDeterministic(t *testing.T) {
	endpoint := contractEndpoint(t, []string{"a", "b", "c"}, "b")

	first, err := routetarget.ResolveRoutableTarget(endpoint)
	if err != nil {
		t.Fatalf("ResolveRoutableTarget returned error: %v", err)
	}
	if got := first.ProviderConfig.Ref().String(); got != "b" {
		t.Fatalf("selected provider config = %q, want %q", got, "b")
	}
}

func contractEndpoint(t *testing.T, refs []string, selectedRef string) endpointintent.Endpoint {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("contract")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	selected, err := endpointintent.ParseProviderConfigRef(selectedRef)
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}

	providerConfigs := make([]endpointintent.ProviderConfig, 0, len(refs))
	for _, rawRef := range refs {
		ref, err := endpointintent.ParseProviderConfigRef(rawRef)
		if err != nil {
			t.Fatalf("ParseProviderConfigRef(%q) returned error: %v", rawRef, err)
		}
		providerConfig, err := endpointintent.NewProviderConfig(
			ref,
			spec,
			"https://example.test/v1",
			"cred-1",
			"chat_completions",
		)
		if err != nil {
			t.Fatalf("NewProviderConfig(%q) returned error: %v", rawRef, err)
		}
		providerConfigs = append(providerConfigs, providerConfig)
	}

	endpoint, err := endpointintent.NewEndpoint(name, providerConfigs, selected)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}
