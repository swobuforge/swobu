package httpapi

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func TestDecodeEndpointDocument_PreservesSelectedFrame(t *testing.T) {
	t.Parallel()

	doc := endpointDocument{
		Name:                      "jobs",
		SelectedProviderConfigRef: "cfg-main",
		ProviderConfigs: []providerConfigDocument{
			{
				Ref:           "cfg-main",
				ProviderSpec:  "openai",
				BaseURL:       "https://api.openai.com/v1",
				CredentialRef: "env:OPENAI_API_KEY",
				SelectedFrame: providercatalog.FrameSSEEvent,
				ModelID:       "gpt-5.4-mini",
			},
		},
	}

	endpoint, err := decodeEndpointDocument(doc)
	if err != nil {
		t.Fatalf("decodeEndpointDocument returned error: %v", err)
	}
	providers := endpoint.ProviderConfigs()
	if len(providers) != 1 {
		t.Fatalf("provider configs len=%d want=1", len(providers))
	}
	if got := providers[0].SelectedFrame(); got != providercatalog.FrameSSEEvent {
		t.Fatalf("selected frame=%q want=%q", got, providercatalog.FrameSSEEvent)
	}
}

func TestEncodeEndpointDocument_PreservesSelectedFrame(t *testing.T) {
	t.Parallel()

	name, _ := endpointintent.ParseEndpointName("jobs")
	ref, _ := endpointintent.ParseProviderConfigRef("cfg-main")
	spec, _ := endpointintent.ParseProviderSpec("openai")
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://api.openai.com/v1", "env:OPENAI_API_KEY")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfg, err = cfg.WithSelectedFrame(providercatalog.FrameSSEEvent)
	if err != nil {
		t.Fatalf("WithSelectedFrame returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}

	doc := encodeEndpointDocument(endpoint)
	if len(doc.ProviderConfigs) != 1 {
		t.Fatalf("provider configs len=%d want=1", len(doc.ProviderConfigs))
	}
	if got := doc.ProviderConfigs[0].SelectedFrame; got != providercatalog.FrameSSEEvent {
		t.Fatalf("selected frame=%q want=%q", got, providercatalog.FrameSSEEvent)
	}
}
