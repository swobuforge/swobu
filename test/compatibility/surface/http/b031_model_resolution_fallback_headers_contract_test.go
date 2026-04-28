package http_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/ports"
)

func TestB031_ModelResolutionFallbackHeadersOnRealRequestPaths(t *testing.T) {
	t.Run("explicit primary selector resolves selected target with default_primary header", func(t *testing.T) {
		endpoint := contractModelEndpoint(t, []contractModelProvider{
			{ref: "backend-a", modelID: "model-primary"},
			{ref: "backend-b", modelID: "model-secondary"},
		}, "backend-a")
		provider := &contractModelProviderExecutor{
			resp: ports.NewBufferedExecuteResponse(
				compatibility.NewConversationOutput(
					"chatcmpl_0",
					"model-primary",
					[]compatibility.OutputItem{compatibility.NewTextOutputItem("text_0", "ok")},
					"stop",
				),
			),
		}
		handler := httpapi.NewHandler(requestpath.NewRequestHandler(contractEndpointReader{endpoint: endpoint}, provider, nil, nil))
		req := httptest.NewRequest(
			http.MethodPost,
			"/c/alpha/chat/completions",
			bytes.NewBufferString(`{"model":"`+compatibility.PrimaryTargetSelector+`","messages":[{"role":"user","content":"hi"}]}`),
		)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("X-Swobu-Model-Resolved"); got != "model-primary" {
			t.Fatalf("resolved header = %q, want %q", got, "model-primary")
		}
		if got := rec.Header().Get("X-Swobu-Model-Resolution"); got != "default_primary" {
			t.Fatalf("resolution header = %q, want %q", got, "default_primary")
		}
	})

	t.Run("missing client model uses selected primary with explicit headers", func(t *testing.T) {
		endpoint := contractModelEndpoint(t, []contractModelProvider{{ref: "backend-a", modelID: "model-default"}}, "backend-a")
		provider := &contractModelProviderExecutor{
			resp: ports.NewBufferedExecuteResponse(
				compatibility.NewConversationOutput(
					"chatcmpl_1",
					"model-default",
					[]compatibility.OutputItem{compatibility.NewTextOutputItem("text_0", "ok")},
					"stop",
				),
			),
		}
		handler := httpapi.NewHandler(requestpath.NewRequestHandler(contractEndpointReader{endpoint: endpoint}, provider, nil, nil))
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("X-Swobu-Model-Resolved"); got != "model-default" {
			t.Fatalf("resolved header = %q, want %q", got, "model-default")
		}
		if got := rec.Header().Get("X-Swobu-Model-Resolution"); got != "default_missing" {
			t.Fatalf("resolution header = %q, want %q", got, "default_missing")
		}
	})

	t.Run("unknown explicit model selector falls back to selected target", func(t *testing.T) {
		endpoint := contractModelEndpoint(t, []contractModelProvider{{ref: "backend-a", modelID: "shared-model"}, {ref: "backend-b", modelID: "shared-model"}}, "backend-a")
		provider := &contractModelProviderExecutor{
			resp: ports.NewBufferedExecuteResponse(
				compatibility.NewConversationOutput(
					"chatcmpl_2",
					"shared-model",
					[]compatibility.OutputItem{compatibility.NewTextOutputItem("text_0", "ok")},
					"stop",
				),
			),
		}
		handler := httpapi.NewHandler(requestpath.NewRequestHandler(contractEndpointReader{endpoint: endpoint}, provider, nil, nil))
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/v1/chat/completions", bytes.NewBufferString(`{"model":"unknown-selector","messages":[{"role":"user","content":"hi"}]}`))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("X-Swobu-Model-Resolved"); got != "shared-model" {
			t.Fatalf("resolved header = %q, want %q", got, "shared-model")
		}
		if got := rec.Header().Get("X-Swobu-Model-Resolution"); got != "default_unknown" {
			t.Fatalf("resolution header = %q, want %q", got, "default_unknown")
		}
	})

	t.Run("provider:model selector resolves matching target", func(t *testing.T) {
		endpoint := contractModelEndpoint(t, []contractModelProvider{
			{ref: "backend-a", providerSpec: "custom", modelID: "gpt-4.1-mini"},
			{ref: "backend-b", providerSpec: "openai", modelID: "gpt-5.3"},
		}, "backend-a")
		provider := &contractModelProviderExecutor{
			resp: ports.NewBufferedExecuteResponse(
				compatibility.NewConversationOutput(
					"chatcmpl_2b",
					"gpt-5.3",
					[]compatibility.OutputItem{compatibility.NewTextOutputItem("text_0", "ok")},
					"stop",
				),
			),
		}
		handler := httpapi.NewHandler(requestpath.NewRequestHandler(contractEndpointReader{endpoint: endpoint}, provider, nil, nil))
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/v1/chat/completions", bytes.NewBufferString(`{"model":"openai:gpt-5.3","messages":[{"role":"user","content":"hi"}]}`))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("X-Swobu-Model-Resolved"); got != "gpt-5.3" {
			t.Fatalf("resolved header = %q, want %q", got, "gpt-5.3")
		}
		if got := rec.Header().Get("X-Swobu-Model-Resolution"); got != "client" {
			t.Fatalf("resolution header = %q, want %q", got, "client")
		}
	})

	t.Run("client model is rejected when selected provider model is unset", func(t *testing.T) {
		endpoint := contractModelEndpoint(t, []contractModelProvider{{ref: "backend-a", modelID: ""}}, "backend-a")
		provider := &contractModelProviderExecutor{
			resp: ports.NewBufferedExecuteResponse(
				compatibility.NewConversationOutput(
					"chatcmpl_3",
					"client-model",
					[]compatibility.OutputItem{compatibility.NewTextOutputItem("text_0", "ok")},
					"stop",
				),
			),
		}
		handler := httpapi.NewHandler(requestpath.NewRequestHandler(contractEndpointReader{endpoint: endpoint}, provider, nil, nil))
		req := httptest.NewRequest(http.MethodPost, "/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"client-model","messages":[{"role":"user","content":"hi"}]}`))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if provider.called {
			t.Fatal("provider execute should not be called when selected provider model is unset")
		}
	})

	t.Run("/models advertises primary plus concrete selectors", func(t *testing.T) {
		endpoint := contractModelEndpoint(t, []contractModelProvider{
			{ref: "backend-a", modelID: "model-default", alias: "fast"},
			{ref: "backend-b", modelID: "model-other"},
		}, "backend-a")
		handler := httpapi.NewHandler(requestpath.NewRequestHandler(contractEndpointReader{endpoint: endpoint}, &contractModelProviderExecutor{}, nil, nil))
		req := httptest.NewRequest(http.MethodGet, "/c/alpha/v1/models", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		body := rec.Body.String()
		for _, id := range []string{"primary", "fast", "model-other"} {
			if !strings.Contains(body, `"id":"`+id+`"`) {
				t.Fatalf("body = %q, want id %q", body, id)
			}
		}
		if strings.Contains(body, `"id":"model-default"`) {
			t.Fatalf("body = %q, model with alias must not appear under mechanical id", body)
		}
	})
}

type contractModelProvider struct {
	ref          string
	providerSpec string
	modelID      string
	alias        string
}

func contractModelEndpoint(t *testing.T, providers []contractModelProvider, selectedRef string) endpointintent.Endpoint {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	selected, err := endpointintent.ParseProviderConfigRef(selectedRef)
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}

	configs := make([]endpointintent.ProviderConfig, 0, len(providers))
	for _, provider := range providers {
		ref, err := endpointintent.ParseProviderConfigRef(provider.ref)
		if err != nil {
			t.Fatalf("ParseProviderConfigRef(%q) returned error: %v", provider.ref, err)
		}
		providerSpec := provider.providerSpec
		if strings.TrimSpace(providerSpec) == "" {
			providerSpec = "custom"
		}
		spec, err := endpointintent.ParseProviderSpec(providerSpec)
		if err != nil {
			t.Fatalf("ParseProviderSpec(%q) returned error: %v", providerSpec, err)
		}
		cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "", protocolsurface.ChatCompletions)
		if err != nil {
			t.Fatalf("NewProviderConfig(%q) returned error: %v", provider.ref, err)
		}
		cfg, err = cfg.WithModelID(provider.modelID)
		if err != nil {
			t.Fatalf("WithModelID(%q) returned error: %v", provider.modelID, err)
		}
		cfg, err = cfg.WithTargetAlias(provider.alias)
		if err != nil {
			t.Fatalf("WithTargetAlias(%q) returned error: %v", provider.alias, err)
		}
		configs = append(configs, cfg)
	}

	endpoint, err := endpointintent.NewEndpoint(name, configs, selected)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}

type contractModelProviderExecutor struct {
	called bool
	resp   ports.ExecuteResponse
}

func (p *contractModelProviderExecutor) Execute(_ context.Context, _ ports.ExecuteRequest) (ports.ExecuteResponse, error) {
	p.called = true
	return p.resp, nil
}
