package azure

import (
	"testing"

	"github.com/swobuforge/swobu/internal/profile"
)

func TestNewRuntime_UsesAzureProviderIDAndSharedKernel(t *testing.T) {
	t.Parallel()

	bundle := NewRuntime(nil, nil)
	if got := bundle.ProviderID; got != profile.ProviderSpecAzure {
		t.Fatalf("provider id=%q want %q", got, profile.ProviderSpecAzure)
	}
	if bundle.IngressResolver == nil {
		t.Fatal("ingress resolver must not be nil")
	}
	if bundle.ModelCatalogClient == nil {
		t.Fatal("model catalog client must not be nil")
	}
}
