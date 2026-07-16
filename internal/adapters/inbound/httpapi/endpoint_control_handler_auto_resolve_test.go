package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
	responses "github.com/swobuforge/swobu/internal/wire/responses"
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
	probe := func(_ context.Context, _ endpointintent.Endpoint, in exchange.RequestInput) (exchange.RequestOutput, error) {
		_ = in
		if len(attempts) == 0 {
			attempts = append(attempts, "responses/http_json_body")
			return exchange.RequestOutput{}, errors.New("nope")
		}
		attempts = append(attempts, "responses/ndjson")
		doc, _ := testResponseDocumentEncoderForFamily(canonical.ClientFamilyResponses).EncodeResponseDocument(canonical.NewConversationOutput("id", "m", []canonical.OutputItem{canonical.NewTextOutputItem("t", "ok")}, "stop"))
		return exchange.RequestOutput{Response: exchange.NewTransportResponseFromDocument(doc)}, nil
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
	probe := func(_ context.Context, _ endpointintent.Endpoint, in exchange.RequestInput) (exchange.RequestOutput, error) {
		_ = in
		return exchange.RequestOutput{}, errors.New("probe failed")
	}

	resolver := newEndpointAutoProtocolResolver(probe)
	_, err = resolver.resolveOne(context.Background(), name, []endpointintent.ProviderConfig{cfg}, 0, ref)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "" || got == profile.ProviderProtocolAuto {
		t.Fatalf("unexpected error=%q", got)
	}
}

func TestEndpointAutoProtocolResolver_ResolveOne_EncodesResponsesPingRequest(t *testing.T) {
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

	var seen exchange.RequestInput
	probe := func(_ context.Context, _ endpointintent.Endpoint, in exchange.RequestInput) (exchange.RequestOutput, error) {
		seen = in
		return exchange.RequestOutput{}, nil
	}

	resolver := newEndpointAutoProtocolResolver(probe)
	_, err = resolver.resolveOne(context.Background(), name, []endpointintent.ProviderConfig{cfg}, 0, ref)
	if err != nil {
		t.Fatalf("resolveOne error: %v", err)
	}

	if seen.ClientFamily != canonical.ClientFamilyResponses {
		t.Fatalf("client family=%q want responses", seen.ClientFamily)
	}
	if got := seen.Request.Method; got != http.MethodPost {
		t.Fatalf("request method=%q want POST", got)
	}
	if got := seen.Request.URL; got != string(canonical.NormalizedPathResponses) {
		t.Fatalf("request url=%q want %q", got, canonical.NormalizedPathResponses)
	}

	gotBody, err := io.ReadAll(seen.Request.Body)
	if err != nil {
		t.Fatalf("read probe body: %v", err)
	}
	wantDoc, err := responses.EncodeCarrier(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "llama3.1",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "ping")},
	}), delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("responses codec encode: %v", err)
	}
	if !bytes.Equal(gotBody, wantDoc.RawBytes()) {
		t.Fatalf("probe body=%s want %s", string(gotBody), string(wantDoc.RawBytes()))
	}
}

func TestEndpointAutoProtocolResolver_Resolve_SkipsConcreteDefaults(t *testing.T) {
	t.Parallel()

	doc := endpointDocument{
		Name:                      "workspace",
		SelectedProviderConfigRef: "cfg-main",
		ProviderConfigs: []providerConfigDocument{
			{
				Ref:           "cfg-main",
				ProviderSpec:  "bedrock",
				BaseURL:       "https://bedrock-mantle.us-east-1.api.aws/v1",
				CredentialRef: "env:AWS_BEARER_TOKEN_BEDROCK",
				ModelID:       "anthropic.claude-3-5-sonnet-20240620-v1:0",
			},
		},
	}
	endpoint, err := decodeEndpointDocument(doc)
	if err != nil {
		t.Fatalf("decodeEndpointDocument error: %v", err)
	}

	called := false
	probe := func(_ context.Context, _ endpointintent.Endpoint, _ exchange.RequestInput) (exchange.RequestOutput, error) {
		called = true
		return exchange.RequestOutput{}, errors.New("probe should not run for concrete defaults")
	}

	resolver := newEndpointAutoProtocolResolver(probe)
	resolved, err := resolver.Resolve(context.Background(), endpoint, doc)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if called {
		t.Fatal("probe should not be called for a concrete default provider protocol")
	}
	wantProtocol, ok := profile.ResolveConcreteProtocolForAutoAtBoundary("bedrock")
	if !ok {
		t.Fatal("bedrock should have a concrete boundary default")
	}
	gotProtocol := resolved.ProviderConfigs()[0].ProviderProtocol()
	if gotProtocol != wantProtocol {
		t.Fatalf("resolved provider protocol=%q want %q", gotProtocol, wantProtocol)
	}
}
