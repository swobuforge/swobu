package exchange_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	. "github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/session"
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

func TestCurrentSearchFallsBackAndSettledSearchHistoryReentersPrimary(t *testing.T) {
	workspace := capabilityFallbackWorkspace(t)
	store := session.NewMemoryStore()
	transportCounts := map[string]int{}
	var localRequests []carrier.Document

	runtime := capabilityFallbackRuntime{transport: func(_ context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
		transportCounts[target.TargetID]++
		switch {
		case strings.HasPrefix(target.TargetID, "local-"):
			localRequests = append(localRequests, document)
			return provider.DocumentIngress{Document: carrier.NewDocument(
				protocolkind.ChatCompletions, "application/json", nil,
				[]byte(`{"id":"chat_local","model":"local","choices":[{"index":0,"message":{"role":"assistant","content":"local answer"},"finish_reason":"stop"}]}`), carrier.Meta{},
			)}, nil
		case target.TargetID == "paid-openai":
			return provider.DocumentIngress{Document: carrier.NewDocument(
				protocolkind.Responses, "application/json", nil,
				[]byte(`{"id":"resp_paid","model":"paid","status":"completed","output":[{"type":"web_search_call","id":"search_1","status":"completed","action":{"type":"search","queries":["deadline"],"sources":[{"type":"url","url":"https://example.test/rules","title":"Rules"}]}},{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Hosted search answer","annotations":[{"type":"url_citation","url":"https://example.test/rules","title":"Rules","start_index":0,"end_index":6}]}]}]}`), carrier.Meta{},
			)}, nil
		default:
			t.Fatalf("unexpected target %q", target.TargetID)
			return nil, nil
		}
	}}

	ingress := NewIngress(capabilityFallbackWorkspaceLookup{workspace: workspace}, runtime, RuntimePoliciesSpec{
		CheckpointStore: store,
		ResponseIDs:     deterministicResponseIDGenerator{},
	})
	runCapabilityFallbackTurn(t, ingress, workspace, "turn1", `{
		"model":"chat",
		"input":"hello"
	}`)
	if capabilityFallbackLocalTransportCount(transportCounts) != 1 || transportCounts["paid-openai"] != 0 {
		t.Fatalf("turn 1 transports = %#v", transportCounts)
	}

	runCapabilityFallbackTurn(t, ingress, workspace, "turn2", `{
		"model":"chat",
		"previous_response_id":"swobu_turn1",
		"tools":[{"type":"web_search"}],
		"input":"find the deadline"
	}`)
	if capabilityFallbackLocalTransportCount(transportCounts) != 1 || transportCounts["paid-openai"] != 1 {
		t.Fatalf("turn 2 transports = %#v; local incompatibility must happen before I/O", transportCounts)
	}

	runCapabilityFallbackTurn(t, ingress, workspace, "turn3", `{
		"model":"chat",
		"previous_response_id":"swobu_turn2",
		"input":"explain that answer"
	}`)
	if capabilityFallbackLocalTransportCount(transportCounts) != 2 || transportCounts["paid-openai"] != 1 {
		t.Fatalf("turn 3 transports = %#v", transportCounts)
	}
	if len(localRequests) != 2 {
		t.Fatalf("local requests = %d, want ordinary turn and historical-search re-entry", len(localRequests))
	}
	reentry := localRequests[1].RawBytes()
	for _, wanted := range [][]byte{[]byte("Hosted search answer"), []byte("explain that answer")} {
		if !bytes.Contains(reentry, wanted) {
			t.Fatalf("local re-entry request missing %q: %s", wanted, reentry)
		}
	}
	for _, forbidden := range [][]byte{[]byte("web_search"), []byte("search_1"), []byte("example.test/rules")} {
		if bytes.Contains(reentry, forbidden) {
			t.Fatalf("local re-entry leaked settled search machinery %q: %s", forbidden, reentry)
		}
	}

	runCapabilityFallbackTurn(t, ingress, workspace, "turn4", `{
		"model":"chat",
		"previous_response_id":"swobu_turn3",
		"tools":[{"type":"web_search"}],
		"input":"search again"
	}`)
	if capabilityFallbackLocalTransportCount(transportCounts) != 2 || transportCounts["paid-openai"] != 2 {
		t.Fatalf("turn 4 transports = %#v", transportCounts)
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
