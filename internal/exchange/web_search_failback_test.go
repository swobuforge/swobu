package exchange_test

import (
	"bytes"
	"context"
	"io"
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
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/wire/responses"
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
	if target.ProtocolKind == protocolkind.Responses {
		codec = protocolcodec.Codec{
			Protocol: protocolkind.Responses,
			ResponsesDialect: protocolcodec.ResponsesDialect{
				Tools: responses.ToolLowering{WebSearch: protocolcodec.ResponsesHostedSearchTool("web_search", false)},
			},
		}
	}
	return provider.Backend{
		Target:    target,
		Codec:     codec,
		Transport: provider.BindTransport(target, r.transport),
	}, nil
}

func TestCurrentSearchGrammarOmissionReachesPrimaryWithoutFallback(t *testing.T) {
	local := capabilityFallbackTarget(t, "local", "chat_completions")
	fallback := capabilityFallbackTarget(t, "fallback", "responses")
	workspace := capabilityFallbackWorkspace(t, "search-omission", []routing.Target{local}, []routing.Target{fallback})
	transportCounts := map[string]int{}
	var primaryRequest carrier.Document

	runtime := capabilityFallbackRuntime{transport: func(_ context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
		transportCounts[target.TargetID]++
		if target.TargetID != "local" {
			t.Fatalf("grammar omission advanced to fallback target %q", target.TargetID)
		}
		primaryRequest = document
		return capabilityFallbackChatResponse("local answer"), nil
	}}
	ingress := capabilityFallbackIngress(workspace, session.NewMemoryStore(), runtime)
	out := runCapabilityFallbackTurn(t, ingress, workspace, "omission", `{
		"model":"chat",
		"tools":[{"type":"web_search"}],
		"input":"find the deadline"
	}`)

	if transportCounts["local"] != 1 || transportCounts["fallback"] != 0 {
		t.Fatalf("transports = %#v, want primary once and fallback zero", transportCounts)
	}
	if bytes.Contains(primaryRequest.RawBytes(), []byte("web_search")) {
		t.Fatalf("generic Chat projection leaked web_search: %s", primaryRequest.RawBytes())
	}
	assertCapabilityOmission(t, out, canonical.RequestToolsKind)
}

func TestNativeSearchProviderUnauthorizedAdvancesToFallback(t *testing.T) {
	native := capabilityFallbackTarget(t, "native-search", "responses")
	fallback := capabilityFallbackTarget(t, "local-fallback", "chat_completions")
	workspace := capabilityFallbackWorkspace(t, "search-rejection", []routing.Target{native}, []routing.Target{fallback})
	var transportOrder []string
	nativeCalls := 0

	runtime := capabilityFallbackRuntime{transport: func(_ context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
		transportOrder = append(transportOrder, target.TargetID)
		switch target.TargetID {
		case "native-search":
			nativeCalls++
			if !bytes.Contains(document.RawBytes(), []byte(`"type":"web_search"`)) {
				t.Fatalf("native search rejection lacked search wire: %s", document.RawBytes())
			}
			// Ollama 0.33.1 returns this shape after its unconfigured private
			// /api/experimental/web_search integration rejects the search.
			return nil, canonical.NewBackendError(target.TargetID, 401, "something went wrong", "")
		case "local-fallback":
			if bytes.Contains(document.RawBytes(), []byte("web_search")) {
				t.Fatalf("fallback Chat projection leaked web_search: %s", document.RawBytes())
			}
			return capabilityFallbackChatResponse("fallback answer"), nil
		default:
			t.Fatalf("unexpected target %q", target.TargetID)
			return nil, nil
		}
	}}
	ingress := capabilityFallbackIngress(workspace, session.NewMemoryStore(), runtime)
	out := runCapabilityFallbackTurn(t, ingress, workspace, "rejection", `{
		"model":"chat",
		"tools":[{"type":"web_search"}],
		"input":"find the deadline"
	}`)

	if nativeCalls != 1 || len(transportOrder) != 2 || transportOrder[0] != "native-search" || transportOrder[1] != "local-fallback" {
		t.Fatalf("transport order = %#v, want native search rejection before fallback", transportOrder)
	}
	if out.Target.TargetID != "local-fallback" {
		t.Fatalf("winning target = %q, want local-fallback", out.Target.TargetID)
	}
}

func TestSettledSearchHistoryReentersLocalTarget(t *testing.T) {
	store := session.NewMemoryStore()
	searchTarget := capabilityFallbackTarget(t, "native-search", "responses")
	searchWorkspace := capabilityFallbackWorkspace(t, "search-history", []routing.Target{searchTarget})
	searchRuntime := capabilityFallbackRuntime{transport: func(_ context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
		if !bytes.Contains(document.RawBytes(), []byte(`"type":"web_search"`)) {
			t.Fatalf("search-producing turn lacked native search wire: %s", document.RawBytes())
		}
		return capabilityFallbackSearchResponse(), nil
	}}
	searchIngress := capabilityFallbackIngress(searchWorkspace, store, searchRuntime)
	runCapabilityFallbackTurn(t, searchIngress, searchWorkspace, "search", `{
		"model":"chat",
		"tools":[{"type":"web_search"}],
		"input":"find the deadline"
	}`)

	localTarget := capabilityFallbackTarget(t, "local", "chat_completions")
	localWorkspace := capabilityFallbackWorkspace(t, "search-history", []routing.Target{localTarget})
	var reentry carrier.Document
	localRuntime := capabilityFallbackRuntime{transport: func(_ context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
		if target.TargetID != "local" {
			t.Fatalf("historical continuation selected %q, want local", target.TargetID)
		}
		reentry = document
		return capabilityFallbackChatResponse("local continuation"), nil
	}}
	localIngress := capabilityFallbackIngress(localWorkspace, store, localRuntime)
	runCapabilityFallbackTurn(t, localIngress, localWorkspace, "continue", `{
		"model":"chat",
		"previous_response_id":"swobu_search",
		"input":"explain that answer"
	}`)

	for _, wanted := range [][]byte{[]byte("Hosted search answer"), []byte("explain that answer")} {
		if !bytes.Contains(reentry.RawBytes(), wanted) {
			t.Fatalf("local re-entry request missing %q: %s", wanted, reentry.RawBytes())
		}
	}
	for _, forbidden := range [][]byte{[]byte("web_search"), []byte("search_1"), []byte("example.test/rules")} {
		if bytes.Contains(reentry.RawBytes(), forbidden) {
			t.Fatalf("local re-entry leaked settled search machinery %q: %s", forbidden, reentry.RawBytes())
		}
	}
}

func capabilityFallbackIngress(workspace routing.Workspace, store session.Store, runtime capabilityFallbackRuntime) RequestIngress {
	return NewIngress(capabilityFallbackWorkspaceLookup{workspace: workspace}, runtime, RuntimePoliciesSpec{
		CheckpointStore: store,
		ResponseIDs:     deterministicResponseIDGenerator{},
	})
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

func assertCapabilityOmission(t *testing.T, out RequestOutput, capability canonical.CapabilityPath) {
	t.Helper()
	if out.Compatibility == nil {
		t.Fatal("response compatibility completion is nil")
	}
	changes := out.Compatibility.Snapshot().Changes
	want := compat.NewOmission(capability, canonical.ToolOccurrence(canonical.WebSearchToolKey()))
	for _, change := range changes {
		if change == want {
			return
		}
	}
	t.Fatalf("compatibility changes = %#v, want %#v", changes, want)
}

func capabilityFallbackChatResponse(text string) provider.Ingress {
	return provider.DocumentIngress{Document: carrier.NewDocument(
		protocolkind.ChatCompletions, "application/json", nil,
		[]byte(`{"id":"chat_local","model":"local","choices":[{"index":0,"message":{"role":"assistant","content":"`+text+`"},"finish_reason":"stop"}]}`), carrier.Meta{},
	)}
}

func capabilityFallbackSearchResponse() provider.Ingress {
	return provider.DocumentIngress{Document: carrier.NewDocument(
		protocolkind.Responses, "application/json", nil,
		[]byte(`{"id":"resp_search","model":"paid","status":"completed","output":[{"type":"web_search_call","id":"search_1","status":"completed","action":{"type":"search","queries":["deadline"],"sources":[{"type":"url","url":"https://example.test/rules","title":"Rules"}]}},{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Hosted search answer","annotations":[{"type":"url_citation","url":"https://example.test/rules","title":"Rules","start_index":0,"end_index":6}]}]}]}`), carrier.Meta{},
	)}
}

func capabilityFallbackWorkspace(t *testing.T, slugName string, tiers ...[]routing.Target) routing.Workspace {
	t.Helper()
	routeTiers := make([]routing.Tier, 0, len(tiers))
	for _, targets := range tiers {
		tier, err := routing.NewTier(targets)
		if err != nil {
			t.Fatal(err)
		}
		routeTiers = append(routeTiers, tier)
	}
	routeName, _ := routing.ParseRouteName("chat")
	route, err := routing.NewRoute(routeName, routeTiers)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug(slugName)
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
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
