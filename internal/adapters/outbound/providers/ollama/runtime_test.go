package ollama

import (
	"testing"

	"github.com/swobuforge/swobu/internal/profile"
)

func TestNewRuntime_BindsOllamaProviderID(t *testing.T) {
	rt := NewRuntime(nil, nil)
	if rt.ProviderID != profile.ProviderSpecOllama {
		t.Fatalf("provider id = %s, want %s", rt.ProviderID, profile.ProviderSpecOllama)
	}
}
