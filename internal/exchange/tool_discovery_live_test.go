//go:build integration_live

package exchange

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	providersadapter "github.com/swobuforge/swobu/internal/adapters/outbound/providers"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type capturingExecutionRuntime struct {
	backends provider.BackendResolver
	document *carrier.Document
}

func (r capturingExecutionRuntime) ClientCodec(canonical.ClientFamily) ClientCodec {
	return testClientCodec{}
}

func (r capturingExecutionRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.backends.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	transport := backend.Transport
	backend.Transport = provider.TransportFunc(func(ctx context.Context, document carrier.Document) (provider.Ingress, error) {
		*r.document = document.Clone()
		return transport.Send(ctx, document)
	})
	return backend, backend.Validate()
}

func (r capturingExecutionRuntime) ResolveTargetSupport(target provider.TargetSnapshot) provider.TargetSupport {
	if resolver, ok := r.backends.(provider.TargetSupportResolver); ok {
		return resolver.ResolveTargetSupport(target)
	}
	return provider.TargetSupport{}
}

func TestLiveBedrockMantleResponsesToolDiscoveryPolyfillThroughExchange(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_BEDROCK_DOGFOOD")) != "1" {
		t.Skip("set SWOBU_LIVE_BEDROCK_DOGFOOD=1 to probe live Bedrock Mantle tool-discovery polyfill")
	}
	region := firstLiveValue(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	model := firstLiveValue(os.Getenv("SWOBU_BEDROCK_MODEL"), "xai.grok-4.3")
	endpoint := firstLiveValue(os.Getenv("SWOBU_BEDROCK_MANTLE_ENDPOINT"), "https://bedrock-mantle."+region+".api.aws/openai/v1")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	function := canonicaltest.MustFunctionTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "get_weather"),
		"Get weather",
		canonicaltest.Schema(t, `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
		canonical.Unspecified[bool](),
	)
	discovery, err := canonical.NewToolDiscoveryTool("find tools", canonicaltest.Schema(t, `{"type":"object"}`), canonical.DiscoveryExecutorProvider)
	if err != nil {
		t.Fatal(err)
	}
	allTools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{discovery, function})
	declarations, _ := canonical.NewToolDeclarationsItem(allTools, canonical.ContextScopeRequest)
	callID, _ := canonical.NewToolCallID("search_1")
	input, _ := canonical.ParseJSONObject([]byte(`{"query":"weather"}`))
	call, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(input), canonical.DiscoveryExecutorProvider)
	loaded, _ := canonical.NewToolSet([]canonical.ToolDeclaration{function})
	result, _ := canonical.NewToolDiscoveryResultItem(callID, loaded, canonical.DiscoveryExecutorProvider)
	functionKey := function.Key()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify(model),
		Items:      []canonical.CanonicalItem{declarations, call, result, canonicaltest.Message(t, canonical.MessageRoleUser, "Call get_weather for London.")},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &functionKey)),
	})
	prepared, err := session.Begin(request)
	if err != nil {
		t.Fatal(err)
	}
	target := liveBedrockRoutingTarget(t, region, endpoint, model)
	workspace := liveSingleTargetWorkspace(t, target)
	registry, err := providersadapter.NewProviderRegistry(http.DefaultClient, credentials.NewEnvResolver())
	if err != nil {
		t.Fatal(err)
	}
	var sent carrier.Document
	runner := runtimeBundle{
		Runtime:         capturingExecutionRuntime{backends: registry, document: &sent},
		CheckpointStore: session.NewMemoryStore(), ResponseIDs: deterministicResponseIDGenerator{}, Policy: DefaultWorkspacePolicy(),
	}
	state := exchangeState{
		input:    exchangeInput{exchangeID: "live-bedrock-tool-discovery", clientFamily: canonical.ClientFamilyResponses, clientDelivery: delivery.BufferedDelivery(), request: request, requestFingerprint: testHistoryRequest([]byte("live-bedrock-tool-discovery")), workspace: workspace},
		prepared: &prepared,
		route:    newRoutePlan(workspace.DefaultRoute(), []routing.Target{target}),
	}
	providerCall, snapshot, _, _, err := prepareProviderCall(ctx, state, providerCallSelection{candidateIndex: 0, requestChoice: providerRequestFullHistory}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if got := providerCall.request.TargetSupport.Get(canonical.RequestToolsDiscovery); got != provider.SupportUnknown {
		t.Fatalf("Bedrock discovery support=%v want unknown", got)
	}
	ingress, err := providerCall.backend.Transport.Send(ctx, providerCall.document)
	if err != nil {
		t.Fatalf("live Grok exchange request failed over %s: %v", endpoint, err)
	}
	defer func() {
		if stream, ok := ingress.(provider.StreamIngress); ok {
			_ = stream.Stream.Body.Close()
		}
	}()
	if bytes.Contains(sent.RawBytes(), []byte("tool_search")) {
		t.Fatalf("exchange polyfill retained tool_search wire: %s", sent.RawBytes())
	}
	decoded, _, err := decodeProviderIngress(ctx, providerCall, ingress, providerCall.backend)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(ctx, decoded.Stream, canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range response.Items() {
		if toolCall, ok := item.ToolCall(); ok && toolCall.Tool() == functionKey {
			t.Logf("live exchange decoded get_weather call from %s", snapshot.TargetID)
			return
		}
	}
	t.Fatalf("live exchange response did not contain canonical ToolCall(get_weather): %#v", response.Items())
}

func liveBedrockRoutingTarget(t *testing.T, region, endpoint, model string) routing.Target {
	t.Helper()
	regionValue, err := routing.ParseBedrockRegion(region)
	if err != nil {
		t.Fatal(err)
	}
	provider, _ := routing.ParseProvider("bedrock", func(candidate string) bool { return candidate == "bedrock" })
	connection, err := routing.NewBedrockConnection(provider, regionValue, endpoint, "")
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := routing.ParseProtocol("responses", provider, func(routing.Provider, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	targetID, _ := routing.ParseTargetID("live-bedrock-tool-discovery")
	upstreamModel, _ := routing.ParseUpstreamModel(model)
	target, err := routing.NewTarget(targetID, upstreamModel, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func liveSingleTargetWorkspace(t *testing.T, target routing.Target) routing.Workspace {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug("live")
	routeName, _ := routing.ParseRouteName("live")
	tier, _ := routing.NewTier([]routing.Target{target})
	route, _ := routing.NewRoute(routeName, []routing.Tier{tier})
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func firstLiveValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
