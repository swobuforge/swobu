package openaicompat

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func TestNewRuntime_BindsOpenAICompatibleProviderID(t *testing.T) {
	rt := NewRuntime(nil, nil)
	if rt.ProviderID != providercatalog.ProviderSpecOpenAICompatible {
		t.Fatalf("provider id = %s, want %s", rt.ProviderID, providercatalog.ProviderSpecOpenAICompatible)
	}
}
