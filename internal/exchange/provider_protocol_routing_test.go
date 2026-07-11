package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestResolveProviderProtocolRouting(t *testing.T) {
	t.Parallel()

	target := NewRoutableTarget("ref", "openai", "https://api.openai.com/v1", "", protocolkind.Responses, "bearer", "", "responses_stream")
	routing, err := ResolveProviderProtocolRouting(target, "protocol must be concrete")
	if err != nil {
		t.Fatalf("ResolveProviderProtocolRouting() error = %v", err)
	}
	if routing.Kind != protocolkind.Responses {
		t.Fatalf("kind=%q", routing.Kind)
	}
	if routing.Delivery.Mode != delivery.Streaming {
		t.Fatalf("delivery mode=%v, want streaming", routing.Delivery.Mode)
	}
	if routing.Delivery.Framing != delivery.FramingSSE {
		t.Fatalf("delivery framing=%q, want sse", routing.Delivery.Framing)
	}
}

func TestProviderRequestPathForProtocol(t *testing.T) {
	t.Parallel()

	path, err := ProviderRequestPathForProtocol(protocolkind.ChatCompletions)
	if err != nil {
		t.Fatalf("ProviderRequestPathForProtocol() error = %v", err)
	}
	if path != "/chat/completions" {
		t.Fatalf("path=%q", path)
	}
}
