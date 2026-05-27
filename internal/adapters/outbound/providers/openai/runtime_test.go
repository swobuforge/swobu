package openai

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func TestNewRuntime_BindsOpenAIProviderID(t *testing.T) {
	rt := NewRuntime(nil, nil)
	if rt.ProviderID != providercatalog.ProviderSpecOpenAI {
		t.Fatalf("provider id = %s, want %s", rt.ProviderID, providercatalog.ProviderSpecOpenAI)
	}
}
