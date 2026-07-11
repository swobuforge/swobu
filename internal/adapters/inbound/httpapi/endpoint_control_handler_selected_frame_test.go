package httpapi

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestDecodeEndpointDocument_PreservesProviderProtocol(t *testing.T) {
	t.Parallel()

	doc := endpointDocument{
		Name:                      "jobs",
		SelectedProviderConfigRef: "cfg-main",
		ProviderConfigs: []providerConfigDocument{
			{
				Ref:              "cfg-main",
				ProviderSpec:     "openai",
				BaseURL:          "https://api.openai.com/v1",
				CredentialRef:    "env:OPENAI_API_KEY",
				ProviderProtocol: "responses_stream",
				ModelID:          "gpt-5.4-mini",
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
	if got := providers[0].ProviderProtocol(); got != "responses_stream" {
		t.Fatalf("provider protocol=%q want=%q", got, "responses_stream")
	}
}

func TestEncodeEndpointDocument_PreservesProviderProtocol(t *testing.T) {
	t.Parallel()

	name, _ := endpointintent.ParseEndpointName("jobs")
	ref, _ := endpointintent.ParseProviderConfigRef("cfg-main")
	spec, _ := endpointintent.ParseProviderSpec("openai")
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://api.openai.com/v1", "env:OPENAI_API_KEY")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfg, err = cfg.WithProviderProtocol("responses_stream")
	if err != nil {
		t.Fatalf("WithProviderProtocol returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}

	doc := encodeEndpointDocument(endpoint)
	if len(doc.ProviderConfigs) != 1 {
		t.Fatalf("provider configs len=%d want=1", len(doc.ProviderConfigs))
	}
	if got := doc.ProviderConfigs[0].ProviderProtocol; got != "responses_stream" {
		t.Fatalf("provider protocol=%q want=%q", got, "responses_stream")
	}
}

func TestDecodeEndpointDocument_AcceptsProtocolAutoAsUnspecified(t *testing.T) {
	t.Parallel()

	doc := endpointDocument{
		Name:                      "jobs",
		SelectedProviderConfigRef: "cfg-main",
		ProviderConfigs: []providerConfigDocument{
			{
				Ref:              "cfg-main",
				ProviderSpec:     "openai",
				BaseURL:          "https://api.openai.com/v1",
				CredentialRef:    "env:OPENAI_API_KEY",
				ProviderProtocol: profile.ProviderProtocolAuto,
				ModelID:          "gpt-5.4-mini",
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
	if got := providers[0].ProviderProtocol(); got == profile.ProviderProtocolAuto || got == "" {
		t.Fatalf("provider protocol=%q want concrete default protocol", got)
	}
}
