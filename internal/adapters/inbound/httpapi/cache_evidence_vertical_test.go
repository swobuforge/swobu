package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	trafficevidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/trafficevidence"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

type cacheEvidenceTransport struct{ call int }

func (t *cacheEvidenceTransport) Send(_ context.Context, target provider.TargetSnapshot, _ carrier.Document) (provider.Ingress, error) {
	t.call++
	cacheUsage := `"input_tokens_details":{"cache_write_tokens":5}`
	if t.call == 2 {
		cacheUsage = `"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":7}`
	}
	raw := []byte(`{"id":"provider-response","model":"model","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":12,"output_tokens":2,` + cacheUsage + `}}`)
	return provider.DocumentIngress{Document: carrier.NewDocument(target.ProtocolKind, "application/json", nil, raw, carrier.Meta{})}, nil
}

type cacheEvidenceRuntime struct {
	codecresolver.RuntimeCodecResolver
	transport *cacheEvidenceTransport
}

func (r cacheEvidenceRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	return provider.Backend{Target: target, Codec: protocolcodec.Codec{Protocol: target.ProtocolKind}, Transport: provider.BindTransport(target, r.transport.Send)}, nil
}

func (cacheEvidenceRuntime) ResolveTargetSupport(provider.TargetSnapshot) provider.TargetSupport {
	return provider.TargetSupport{}
}

func TestCacheEvidenceTraversesExchangeStoreHTTPAndOperatorClient(t *testing.T) {
	restoreLogger, logs := testDebugLogger()
	defer restoreLogger()
	workspace := cacheEvidenceWorkspace(t)
	store := trafficevidencestore.NewTrafficEventStore(trafficevidencestore.StoreConfig{})
	runtime := cacheEvidenceRuntime{RuntimeCodecResolver: codecresolver.NewRuntimeCodecResolver(), transport: &cacheEvidenceTransport{}}
	ingress := exchange.NewIngress(imageIncidentWorkspaceLookup{workspace: workspace}, runtime, exchange.RuntimePoliciesSpec{
		TrafficEvidence: store,
		PolicyResolver:  exchange.StaticWorkspacePolicyResolver{Policy: exchange.DefaultWorkspacePolicy()},
	})

	mux := http.NewServeMux()
	mux.Handle("/c/", NewHandler(ingress, store))
	mux.Handle("/_swobu/status-projection", NewStatusProjectionHandler(func(_ context.Context, scope trafficevidencestore.ProjectionScope) (trafficevidencestore.StatusProjection, error) {
		return store.ProjectStatus(trafficevidencestore.ProjectionInput{State: "running", Scope: scope}), nil
	}))
	server := httptest.NewServer(mux)
	defer server.Close()

	firstID := issueCacheEvidenceRequest(t, server, `{"model":"default","instructions":"be concise","input":"one","stream":false}`)
	issueCacheEvidenceRequest(t, server, `{"model":"default","previous_response_id":"`+firstID+`","instructions":"be expansive","input":"two","stream":false}`)
	statusResponse, err := server.Client().Get(server.URL + "/_swobu/status-projection?scope=all")
	if err != nil {
		t.Fatal(err)
	}
	var statusJSON map[string]any
	if err := json.NewDecoder(statusResponse.Body).Decode(&statusJSON); err != nil {
		_ = statusResponse.Body.Close()
		t.Fatal(err)
	}
	_ = statusResponse.Body.Close()
	statusRaw, _ := json.Marshal(statusJSON)
	if !strings.Contains(string(statusRaw), `"reusable_prefix":{"change_kind":"instruction","state":"changed"}`) {
		t.Fatalf("status JSON lacks reusable-prefix contract: %s", statusRaw)
	}
	for _, legacy := range []string{"stable_segments", "compared_segments", "stable_bytes", "compared_bytes", "first_divergence_kind"} {
		if strings.Contains(string(statusRaw), legacy) {
			t.Fatalf("status JSON retained legacy field %q: %s", legacy, statusRaw)
		}
	}

	projection, err := operatorclient.New(server.Client(), server.URL).Status(context.Background(), "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.RecentTraffic) != 2 {
		t.Fatalf("recent traffic rows = %d", len(projection.RecentTraffic))
	}
	changed, initial := projection.RecentTraffic[0], projection.RecentTraffic[1]
	if changed.TokenUsage == nil || changed.TokenUsage.CacheReadTokens == nil || *changed.TokenUsage.CacheReadTokens != 0 || changed.TokenUsage.CacheWriteTokens == nil || *changed.TokenUsage.CacheWriteTokens != 7 {
		t.Fatalf("changed usage = %#v", changed.TokenUsage)
	}
	if initial.TokenUsage == nil || initial.TokenUsage.CacheReadTokens != nil || initial.TokenUsage.CacheWriteTokens == nil || *initial.TokenUsage.CacheWriteTokens != 5 {
		t.Fatalf("initial usage = %#v", initial.TokenUsage)
	}
	if changed.ReusablePrefix.State != "changed" || changed.ReusablePrefix.ChangeKind != "instruction" || initial.ReusablePrefix.State != "unknown" {
		t.Fatalf("prefix evidence = changed:%#v initial:%#v", changed.ReusablePrefix, initial.ReusablePrefix)
	}
	if changed.TargetProtocol != "responses" || changed.TargetVersion != 1 || changed.AttemptCount != 1 || changed.FallbackRecovered {
		t.Fatalf("execution evidence = %#v", changed)
	}
	logOutput := logs.String()
	for _, expected := range []string{"event=reusable_prefix_changed", "kind=instruction", "occurrence=request:0"} {
		if !strings.Contains(logOutput, expected) {
			t.Fatalf("logs missing %q\n%s", expected, logOutput)
		}
	}
	for _, forbidden := range []string{"be concise", "be expansive"} {
		if strings.Contains(logOutput, forbidden) {
			t.Fatalf("prompt material %q reached logs", forbidden)
		}
	}
}

func issueCacheEvidenceRequest(t *testing.T, server *httptest.Server, body string) string {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/c/cache/responses", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("request status = %d", response.StatusCode)
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil || payload.ID == "" {
		t.Fatalf("response id = %q (%v)", payload.ID, err)
	}
	return payload.ID
}

func cacheEvidenceWorkspace(t *testing.T) routing.Workspace {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug("cache")
	routeName, _ := routing.ParseRouteName("cache-route")
	targetID, _ := routing.ParseTargetID("cache-target")
	model, _ := routing.ParseUpstreamModel("model")
	providerID, _ := routing.ParseProvider("custom", func(raw string) bool { return raw == "custom" })
	connection, _ := routing.NewCustomConnection(providerID, "https://example.test/v1", nil)
	protocol, _ := routing.ParseProtocol("responses", providerID, func(routing.Provider, string) bool { return true })
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	tier, _ := routing.NewTier([]routing.Target{target})
	route, _ := routing.NewRoute(routeName, []routing.Tier{tier})
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}
