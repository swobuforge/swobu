package openai

import (
	"testing"

	"github.com/swobuforge/swobu/internal/profile"
)

func TestNewRuntime_BindsOpenAIProviderID(t *testing.T) {
	rt := NewRuntime(nil, nil)
	if rt.ProviderID != profile.ProviderSpecOpenAI {
		t.Fatalf("provider id = %s, want %s", rt.ProviderID, profile.ProviderSpecOpenAI)
	}
}
