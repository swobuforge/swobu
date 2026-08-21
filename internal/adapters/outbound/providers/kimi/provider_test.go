package kimi

import (
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestRuntimeUsesSharedKimiPolicy(t *testing.T) {
	bundle := NewRuntime(http.DefaultClient, nil)
	target := provider.NewTargetSnapshot("kimi", string(profile.ProviderSpecKimi), "https://api.moonshot.ai/v1", "env:MOONSHOT_API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	target.Model = "kimi-k3"
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	if codec, ok := backend.Codec.(protocolcodec.Codec); !ok || codec.ChatDialect.ResponseReasoning == nil {
		t.Fatalf("codec = %T, want Kimi reasoning dialect codec", backend.Codec)
	}
	if bundle.Discovery == nil {
		t.Fatal("shared model discovery is missing")
	}
}

var _ providersruntime.Discovery = NewRuntime(nil, nil).Discovery
