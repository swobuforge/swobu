package exchange_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	. "github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

type capabilityFallbackRuntime struct {
	testRuntimeResolver
	transport testProviderTransport
}

type capabilityFallbackWorkspaceLookup struct{ workspace routing.Workspace }

func (l capabilityFallbackWorkspaceLookup) GetWorkspace(context.Context, routing.WorkspaceSlug) (routing.Workspace, error) {
	return l.workspace, nil
}

func (r capabilityFallbackRuntime) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	codec := provider.Codec(testBackendCodec{protocol: target.ProtocolKind})
	if target.TargetID == "paid-openai" {
		codec = protocolcodec.Codec{
			Protocol: protocolkind.Responses,
			ResponsesDialect: protocolcodec.ResponsesDialect{
				LowerTool:       protocolcodec.ResponsesHostedSearchTool("web_search_preview"),
				LowerToolPolicy: protocolcodec.ResponsesHostedSearchToolPolicy("web_search_preview"),
			},
		}
	}
	return provider.Backend{
		Target:    target,
		Codec:     codec,
		Transport: provider.BindTransport(target, r.transport),
	}, nil
}

func TestOptionalWebSearchLossDoesNotAdvanceRoute(t *testing.T) {
	workspace := capabilityFallbackWorkspace(t)
	transportCounts := map[string]int{}
	var localRequest carrier.Document

	runtime := capabilityFallbackRuntime{transport: func(_ context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
		transportCounts[target.TargetID]++
		if !strings.HasPrefix(target.TargetID, "local-") {
			t.Fatalf("optional web search advanced to %q", target.TargetID)
		}
		localRequest = document
		return provider.DocumentIngress{Document: carrier.NewDocument(
			protocolkind.ChatCompletions, "application/json", nil,
			[]byte(`{"id":"chat_local","model":"local","choices":[{"index":0,"message":{"role":"assistant","content":"local answer"},"finish_reason":"stop"}]}`), carrier.Meta{},
		)}, nil
	}}

	ingress := NewIngress(capabilityFallbackWorkspaceLookup{workspace: workspace}, runtime, RuntimePoliciesSpec{
		ResponseIDs: deterministicResponseIDGenerator{},
	})
	output := runCapabilityFallbackTurn(t, ingress, workspace, "soft-web-search", `{
		"model":"chat",
		"tools":[{"type":"web_search"}],
		"input":"find the deadline"
	}`)
	if capabilityFallbackLocalTransportCount(transportCounts) != 1 || transportCounts["paid-openai"] != 0 {
		t.Fatalf("transports = %#v, want one local execution", transportCounts)
	}
	if bytes.Contains(localRequest.RawBytes(), []byte("web_search")) {
		t.Fatalf("omitted web search leaked into local request: %s", localRequest.RawBytes())
	}
	changes := output.Compatibility.Snapshot().Changes
	if len(changes) != 1 || changes[0].Capability != canonical.RequestToolsKind || changes[0].Kind != compat.Omission {
		t.Fatalf("compatibility changes = %#v, want tool-kind omission", changes)
	}
}

func runCapabilityFallbackTurn(t *testing.T, ingress RequestIngress, workspace routing.Workspace, exchangeID, raw string) RequestOutput {
	t.Helper()
	out, err := ingress.HandleRequestWithWorkspace(context.Background(), workspace, RequestInput{
		Workspace: workspace.Slug(), Request: NewTransportRequest("POST", "/v1/responses", nil, []byte(raw)),
		ClientHandler: "capability-fallback-test", ClientFamily: canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingNone, ExchangeID: exchangeID,
	})
	if err != nil {
		t.Fatalf("%s failed: %v", exchangeID, err)
	}
	response := ClientTransportForTest(out.Response)
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("read %s response: %v", exchangeID, err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close %s response: %v", exchangeID, err)
	}
	return out
}

func capabilityFallbackWorkspace(t *testing.T) routing.Workspace {
	t.Helper()
	localA := capabilityFallbackTarget(t, "local-a", "chat_completions")
	localB := capabilityFallbackTarget(t, "local-b", "chat_completions")
	paid := capabilityFallbackTarget(t, "paid-openai", "responses")
	primary, _ := routing.NewTier([]routing.Target{localA, localB})
	fallback, _ := routing.NewTier([]routing.Target{paid})
	routeName, _ := routing.ParseRouteName("chat")
	route, _ := routing.NewRoute(routeName, []routing.Tier{primary, fallback})
	slug, _ := routing.ParseWorkspaceSlug("capability-fallback")
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func capabilityFallbackLocalTransportCount(counts map[string]int) int {
	return counts["local-a"] + counts["local-b"]
}

func capabilityFallbackTarget(t *testing.T, id, protocolName string) routing.Target {
	t.Helper()
	targetID, _ := routing.ParseTargetID(id)
	model, _ := routing.ParseUpstreamModel(id + "-model")
	providerName, _ := routing.ParseProvider("custom", func(candidate string) bool { return candidate == "custom" })
	connection, _ := routing.NewCustomConnection(providerName, "https://example.test/v1", nil)
	protocol, err := routing.ParseProtocol(protocolName, providerName, func(routing.Provider, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	return target
}
