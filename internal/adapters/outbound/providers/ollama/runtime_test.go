package ollama

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func TestNewRuntime_BindsOllamaProviderID(t *testing.T) {
	rt := NewRuntime(nil, nil)
	if rt.ProviderID != providercatalog.ProviderSpecOllama {
		t.Fatalf("provider id = %s, want %s", rt.ProviderID, providercatalog.ProviderSpecOllama)
	}
}
