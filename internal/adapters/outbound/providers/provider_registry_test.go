package providers

import (
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/profile"
)

func TestProviderIngressResolverComposition_BuildsRuntimeBundlesForSupportedSpecs(t *testing.T) {
	t.Parallel()

	composition := NewProviderIngressResolverComposition(http.DefaultClient, testCredentialResolver{}, "")
	for _, spec := range profile.SupportedSpecs() {
		providerID, ok := profile.ParseProviderID(spec)
		if !ok {
			t.Fatalf("supported spec %q did not parse as provider id", spec)
		}
		runtime, err := composition.runtimeForTargetProvider(spec)
		if err != nil {
			t.Fatalf("runtimeForTargetProvider(%q): %v", spec, err)
		}
		if runtime.ProviderID != providerID {
			t.Fatalf("runtime provider id = %q, want %q", runtime.ProviderID, providerID)
		}
		if runtime.IngressResolver == nil {
			t.Fatalf("runtime ingress resolver missing for %q", spec)
		}
		if runtime.CredentialProvider == nil {
			t.Fatalf("runtime credential provider missing for %q", spec)
		}
		if profile.SupportsCapability(spec, profile.CapabilityModelCatalog) && runtime.ModelCatalogClient == nil {
			t.Fatalf("runtime model catalog client missing for %q", spec)
		}
	}
}

func TestProviderIngressResolverComposition_RejectsUnknownProviderID(t *testing.T) {
	t.Parallel()

	composition := NewProviderIngressResolverComposition(http.DefaultClient, testCredentialResolver{}, "")
	if _, err := composition.runtimeForTargetProvider("unknown-provider"); err == nil {
		t.Fatal("unknown provider id must fail")
	}
}
