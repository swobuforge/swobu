package routetarget

import (
	"testing"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
)

func TestResolveSelectedTarget_ReturnsExplicitlySelectedTarget(t *testing.T) {
	endpoint := testEndpoint(t, []providerConfigSpec{
		{ref: "first"},
		{ref: "selected"},
		{ref: "other"},
	}, "selected")

	resolved, err := ResolveRoutableTarget(endpoint)
	if err != nil {
		t.Fatalf("ResolveRoutableTarget returned error: %v", err)
	}
	if got := resolved.ProviderConfig.Ref().String(); got != "selected" {
		t.Fatalf("selected provider config = %q, want %q", got, "selected")
	}
}

func TestResolveRoutableTarget_ReturnsRouteProfile(t *testing.T) {
	endpoint := testEndpoint(t, []providerConfigSpec{
		{ref: "selected"},
	}, "selected")

	resolved, err := ResolveRoutableTarget(endpoint)
	if err != nil {
		t.Fatalf("ResolveRoutableTarget returned error: %v", err)
	}
	if got := resolved.RouteProfile.ProviderSpec; got != "custom" {
		t.Fatalf("route profile provider spec = %q, want custom", got)
	}
}

type providerConfigSpec struct {
	ref string
}

func testEndpoint(t *testing.T, specs []providerConfigSpec, selectedRef string) endpointintent.Endpoint {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	selected, err := endpointintent.ParseProviderConfigRef(selectedRef)
	if err != nil {
		t.Fatalf("ParseProviderConfigRef(%q) returned error: %v", selectedRef, err)
	}

	providerConfigs := make([]endpointintent.ProviderConfig, 0, len(specs))
	for _, entry := range specs {
		ref, err := endpointintent.ParseProviderConfigRef(entry.ref)
		if err != nil {
			t.Fatalf("ParseProviderConfigRef(%q) returned error: %v", entry.ref, err)
		}
		providerConfig, err := endpointintent.NewProviderConfig(
			ref,
			spec,
			"https://example.test/v1",
			"cred-1",
			"chat_completions",
		)
		if err != nil {
			t.Fatalf("NewProviderConfig(%q) returned error: %v", entry.ref, err)
		}
		providerConfigs = append(providerConfigs, providerConfig)
	}

	endpoint, err := endpointintent.NewEndpoint(name, providerConfigs, selected)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}
