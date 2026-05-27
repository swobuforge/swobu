package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

func TestEndpointAutoProtocolResolver_ResolveOne_UsesCatalogOrderAndStopsOnFirstSuccess(t *testing.T) {
	t.Parallel()

	name, _ := endpointintent.ParseEndpointName("workspace")
	ref, _ := endpointintent.ParseProviderConfigRef("cfg-main")
	spec, _ := endpointintent.ParseProviderSpec("ollama")
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "http://localhost:11434", "")
	if err != nil {
		t.Fatalf("new provider config: %v", err)
	}
	cfg, err = cfg.WithModelID("llama3.1")
	if err != nil {
		t.Fatalf("with model id: %v", err)
	}
	attempts := make([]string, 0, 2)
	probe := func(_ context.Context, _ endpointintent.Endpoint, in requestpath.HandleInput) (requestpath.HandleOutput, error) {
		_ = in
		if len(attempts) == 0 {
			attempts = append(attempts, "responses/http_json_body")
			return requestpath.HandleOutput{}, errors.New("nope")
		}
		attempts = append(attempts, "responses/ndjson")
		return requestpath.HandleOutput{Response: ports.NewBufferedProviderResponse(canonical.NewConversationOutput("id", "m", []canonical.OutputItem{canonical.NewTextOutputItem("t", "ok")}, "stop"))}, nil
	}

	resolver := newEndpointAutoProtocolResolver(probe)
	resolved, err := resolver.resolveOne(context.Background(), name, []endpointintent.ProviderConfig{cfg}, 0, ref)
	if err != nil {
		t.Fatalf("resolveOne error: %v", err)
	}
	if resolved != "responses_stream" {
		t.Fatalf("resolved protocol=%q want responses_stream", resolved)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempts=%d want 2", len(attempts))
	}
}

func TestEndpointAutoProtocolResolver_ResolveOne_ReturnsErrorWhenNoVariantWorks(t *testing.T) {
	t.Parallel()

	name, _ := endpointintent.ParseEndpointName("workspace")
	ref, _ := endpointintent.ParseProviderConfigRef("cfg-main")
	spec, _ := endpointintent.ParseProviderSpec("ollama")
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "http://localhost:11434", "")
	if err != nil {
		t.Fatalf("new provider config: %v", err)
	}
	cfg, err = cfg.WithModelID("llama3.1")
	if err != nil {
		t.Fatalf("with model id: %v", err)
	}
	probe := func(_ context.Context, _ endpointintent.Endpoint, in requestpath.HandleInput) (requestpath.HandleOutput, error) {
		_ = in
		return requestpath.HandleOutput{}, errors.New("probe failed")
	}

	resolver := newEndpointAutoProtocolResolver(probe)
	_, err = resolver.resolveOne(context.Background(), name, []endpointintent.ProviderConfig{cfg}, 0, ref)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "" || got == providercatalog.ProviderProtocolAuto {
		t.Fatalf("unexpected error=%q", got)
	}
}
