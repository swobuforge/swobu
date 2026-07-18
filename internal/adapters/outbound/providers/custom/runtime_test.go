package custom

import (
	"testing"

	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestNewRuntime_BindsCustomProviderID(t *testing.T) {
	rt := NewRuntime(nil, nil)
	if rt.ProviderID != profile.ProviderSpecCustom {
		t.Fatalf("provider id = %s, want %s", rt.ProviderID, profile.ProviderSpecCustom)
	}
}

func TestRuntimeFactoryRegisteredOnlyUnderCustomIdentity(t *testing.T) {
	if _, ok := providersruntime.RuntimeFactoryFor(profile.ProviderSpecCustom); !ok {
		t.Fatal("custom runtime factory is not registered")
	}
	if _, ok := providersruntime.RuntimeFactoryFor(profile.ProviderID("openai_" + "compatible")); ok {
		t.Fatal("obsolete custom-endpoint runtime factory must not be registered")
	}
}
