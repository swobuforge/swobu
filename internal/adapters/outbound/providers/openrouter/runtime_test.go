package openrouter

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func TestNewRuntime_BindsOpenRouterProviderID(t *testing.T) {
	rt := NewRuntime(nil, nil)
	if rt.ProviderID != providercatalog.ProviderSpecOpenRouter {
		t.Fatalf("provider id = %s, want %s", rt.ProviderID, providercatalog.ProviderSpecOpenRouter)
	}
}
