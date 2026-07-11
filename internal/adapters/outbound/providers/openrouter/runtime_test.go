package openrouter

import (
	"testing"

	"github.com/swobuforge/swobu/internal/profile"
)

func TestNewRuntime_BindsOpenRouterProviderID(t *testing.T) {
	rt := NewRuntime(nil, nil)
	if rt.ProviderID != profile.ProviderSpecOpenRouter {
		t.Fatalf("provider id = %s, want %s", rt.ProviderID, profile.ProviderSpecOpenRouter)
	}
}
