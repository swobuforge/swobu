package azure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestNewRuntime_UsesAzureProviderIDAndSharedKernel(t *testing.T) {
	t.Parallel()

	bundle := NewRuntime(nil, nil, "")
	if got := bundle.ProviderID; got != profile.ProviderSpecAzure {
		t.Fatalf("provider id=%q want %q", got, profile.ProviderSpecAzure)
	}
	if bundle.ProviderExecutor == nil {
		t.Fatal("provider executor must not be nil")
	}
	if bundle.Discovery == nil {
		t.Fatal("discovery facet must not be nil")
	}
}

func TestNewPolicy_UsesAzureProviderIDAndApiKeyAuth(t *testing.T) {
	t.Parallel()

	policy := NewPolicy()
	if got := policy.ProviderID(); got != profile.ProviderSpecAzure {
		t.Fatalf("provider id=%q want %q", got, profile.ProviderSpecAzure)
	}
	if got := policy.AuthStrategy().Header; got != "api-key" {
		t.Fatalf("auth header=%q want api-key", got)
	}
}

func TestNewRuntime_UsesAzureProjectDeploymentsEndpointOnProjectEndpoint(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("request method = %s want GET", r.Method)
		}
		if r.URL.Path != "/api/projects/contact-8837/deployments" {
			t.Fatalf("request path = %s want /api/projects/contact-8837/deployments", r.URL.Path)
		}
		if got := r.URL.Query().Get("api-version"); got != "v1" {
			t.Fatalf("api-version query = %q want v1", got)
		}
		if got := r.URL.Query().Get("deploymentType"); got != "ModelDeployment" {
			t.Fatalf("deploymentType query = %q want ModelDeployment", got)
		}
		if got := r.Header.Get("api-key"); got != "token_test" {
			t.Fatalf("api-key header = %q want token_test", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":[{"name":"DeepSeek-V4-Pro","type":"ModelDeployment","modelName":"DeepSeek-V4-Pro","modelVersion":"2026-04-23","modelPublisher":"DeepSeek","capabilities":{"chat_completion":"true"},"sku":{"name":"GlobalStandard","family":"Anthropic","capacity":125}},{"name":"grok-4.3","type":"ModelDeployment","modelName":"grok-4.3","modelVersion":"1","modelPublisher":"xAI","capabilities":{"chat_completion":"true"},"sku":{"name":"GlobalStandard","family":"Anthropic","capacity":125}}]}`))
	}))
	defer srv.Close()

	bundle := NewRuntime(rewritingClientForServer(t, srv), stubCredentialResolver{}, "https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837")
	deployments, err := bundle.Discovery.ListDeployments(context.Background(), exchange.NewRoutableTarget(
		"draft",
		"azure",
		"",
		"env:AZURE_OPENAI_API_KEY",
		protocolkind.ChatCompletions,
		"credential_ref",
		"",
		string(protocolkind.ChatCompletions),
	))
	if err != nil {
		t.Fatalf("ListDeployments returned error: %v", err)
	}
	if len(deployments) != 2 {
		t.Fatalf("deployments=%v want 2", deployments)
	}
	got := deployments[0]
	if got.Name != "DeepSeek-V4-Pro" {
		t.Fatalf("deployment name=%q want DeepSeek-V4-Pro", got.Name)
	}
	if got.ModelName != "DeepSeek-V4-Pro" {
		t.Fatalf("deployment model name=%q want DeepSeek-V4-Pro", got.ModelName)
	}
	if got.ModelPublisher != "DeepSeek" {
		t.Fatalf("deployment publisher=%q want DeepSeek", got.ModelPublisher)
	}
	if got.Family != "openai" {
		t.Fatalf("deployment family=%q want openai", got.Family)
	}
	if len(got.SupportedProviderProtocols) != 4 {
		t.Fatalf("deployment protocols=%v want openai chat protocols", got.SupportedProviderProtocols)
	}
	if got.SupportedProviderProtocols[0] != "responses" || got.DefaultProviderProtocol != "responses" {
		t.Fatalf("deployment protocol selection=%v default=%q want responses", got.SupportedProviderProtocols, got.DefaultProviderProtocol)
	}
}

func TestResolveProviderIngress_UsesAnthropicPathForMessages(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("request method = %s want POST", r.Method)
		}
		if r.URL.Path != "/anthropic/v1/messages" {
			t.Fatalf("request path = %s want /anthropic/v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("api-key"); got != "token_test" {
			t.Fatalf("api-key header = %q want token_test", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()

	bundle := NewRuntime(rewritingClientForServer(t, srv), stubCredentialResolver{}, "https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837")
	req := exchange.NewProviderRequest("test-ex", protocolkind.Responses,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "claude-sonnet-4",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		carrier.WireDocument{},
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget(
			"draft",
			string(profile.ProviderSpecAzure),
			"",
			"env:AZURE_OPENAI_API_KEY",
			protocolkind.Messages,
			"credential_ref",
			"",
			"messages",
		),
	)
	if _, err := bundle.ProviderExecutor.ResolveProviderIngress(context.Background(), req); err != nil {
		t.Fatalf("ResolveProviderIngress returned error: %v", err)
	}
}

func TestNewRuntime_PreservesAzureDeploymentsWithoutMetadata(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("request method = %s want GET", r.Method)
		}
		if r.URL.Path != "/api/projects/contact-8837/deployments" {
			t.Fatalf("request path = %s want /api/projects/contact-8837/deployments", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"name":"DeepSeek-V4-Pro"}]}`))
	}))
	defer srv.Close()

	bundle := NewRuntime(rewritingClientForServer(t, srv), stubCredentialResolver{}, "https://contact-8837-resource.services.ai.azure.com/api/projects/contact-8837")
	deployments, err := bundle.Discovery.ListDeployments(context.Background(), exchange.NewRoutableTarget(
		"draft",
		"azure",
		"",
		"env:AZURE_OPENAI_API_KEY",
		protocolkind.ChatCompletions,
		"credential_ref",
		"",
		"",
	))
	if err != nil {
		t.Fatalf("ListDeployments returned error: %v", err)
	}
	if len(deployments) != 1 {
		t.Fatalf("deployments=%v want 1", deployments)
	}
	if got := deployments[0].Name; got != "DeepSeek-V4-Pro" {
		t.Fatalf("deployment name=%q want DeepSeek-V4-Pro", got)
	}
	if got := deployments[0].ModelName; got != "DeepSeek-V4-Pro" {
		t.Fatalf("deployment model name=%q want DeepSeek-V4-Pro", got)
	}
	if got := deployments[0].Family; got != "" {
		t.Fatalf("deployment family=%q want empty", got)
	}
}

type stubCredentialResolver struct{}

func (stubCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "token_test", nil
}

type rewriteRoundTripper struct {
	base   http.RoundTripper
	target *url.URL
}

func (rt rewriteRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = rt.target.Scheme
	clone.URL.Host = rt.target.Host
	clone.Host = rt.target.Host
	return rt.base.RoundTrip(clone)
}

func rewritingClientForServer(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	return &http.Client{
		Transport: rewriteRoundTripper{
			base:   srv.Client().Transport,
			target: target,
		},
	}
}
