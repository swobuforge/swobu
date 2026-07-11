package openaicompat

import (
	"testing"

	"github.com/swobuforge/swobu/internal/profile"
)

func TestNewRuntime_BindsOpenAICompatibleProviderID(t *testing.T) {
	rt := NewRuntime(nil, nil)
	if rt.ProviderID != profile.ProviderSpecOpenAICompatible {
		t.Fatalf("provider id = %s, want %s", rt.ProviderID, profile.ProviderSpecOpenAICompatible)
	}
}
