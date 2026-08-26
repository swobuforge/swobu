package exchange_test

import (
	"bytes"
	"context"
	"io"
	"reflect"
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

func TestCapabilityFallbackAutomaticallyReentersPrimaryAfterSettledWebSearch(t *testing.T) {
	workspace := capabilityFallbackWorkspace(t)
	store := session.NewMemoryStore()
	transportCounts := map[string]int{}
	var localRequests []carrier.Document
	var localTargets []string

	runtime := capabilityFallbackRuntime{transport: func(_ context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
		transportCounts[target.TargetID]++
		switch {
		case strings.HasPrefix(target.TargetID, "local-"):
			localTargets = append(localTargets, target.TargetID)
			localRequests = append(localRequests, document)
			answer := "local ordinary answer"
			if len(localTargets) == 2 {
				answer = "local re-entry answer"
			}
			return provider.DocumentIngress{Document: carrier.NewDocument(
				protocolkind.ChatCompletions, "application/json", nil,
				[]byte(`{"id":"chat_local","model":"local","choices":[{"index":0,"message":{"role":"assistant","content":"`+answer+`"},"finish_reason":"stop"}]}`), carrier.Meta{},
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

	_ = runCapabilityFallbackTurn(t, ingress, workspace, "turn1", `{
		"model":"chat",
		"input":"hello"
	}`)
	if capabilityFallbackLocalTransportCount(transportCounts) != 1 || transportCounts["paid-openai"] != 0 {
		t.Fatalf("turn 1 transports = %#v", transportCounts)
	}

	_ = runCapabilityFallbackTurn(t, ingress, workspace, "turn2", `{
		"model":"chat",
		"previous_response_id":"swobu_turn1",
		"tools":[{"type":"web_search"}],
		"input":"find the deadline"
	}`)
	if capabilityFallbackLocalTransportCount(transportCounts) != 1 || transportCounts["paid-openai"] != 1 {
		t.Fatalf("turn 2 transports = %#v; local incompatibility must happen before I/O", transportCounts)
	}
	checkpoint, ok, err := store.Get(context.Background(), workspace.Slug().String(), "swobu_turn2")
	if err != nil || !ok {
		t.Fatalf("turn 2 checkpoint = (%t, %v)", ok, err)
	}
	settledBeforeReentry := checkpoint.Response.Items()
	assertSettledWebSearchTruth(t, settledBeforeReentry)

	reentryOutput := runCapabilityFallbackTurn(t, ingress, workspace, "turn3", `{
		"model":"chat",
		"previous_response_id":"swobu_turn2",
		"input":"explain that answer"
	}`)
	if capabilityFallbackLocalTransportCount(transportCounts) != 2 || transportCounts["paid-openai"] != 1 {
		t.Fatalf("turn 3 transports = %#v", transportCounts)
	}
	if len(localTargets) != 2 || localTargets[0] != localTargets[1] {
		t.Fatalf("same-tier locality changed across fallback: %#v", localTargets)
	}
	reentryChanges := reentryOutput.Compatibility.Snapshot().Changes
	if len(reentryChanges) != 2 {
		t.Fatalf("re-entry compatibility changes = %#v, want lifecycle and citation omissions", reentryChanges)
	}
	item, itemOK := reentryChanges[0].Occurrence.RequestItem()
	part, partOK := reentryChanges[1].Occurrence.RequestPart()
	if reentryChanges[0].Capability != canonical.RequestItemsKind || reentryChanges[0].Kind != compat.Omission || !itemOK || item != 3 ||
		reentryChanges[1].Capability != canonical.RequestItemsMessageCitations || reentryChanges[1].Kind != compat.Omission || !partOK || part.Item != 5 || part.Part != 0 {
		t.Fatalf("re-entry compatibility changes = %#v", reentryChanges)
	}
	if len(localRequests) != 2 {
		t.Fatalf("local requests = %d, want turn 1 and turn 3", len(localRequests))
	}
	reentry := localRequests[1].RawBytes()
	for _, want := range [][]byte{[]byte("Hosted search answer"), []byte("explain that answer")} {
		if !bytes.Contains(reentry, want) {
			t.Fatalf("local re-entry request missing %q: %s", want, reentry)
		}
	}
	for _, forbidden := range [][]byte{[]byte("web_search"), []byte("search_1"), []byte("example.test/rules")} {
		if bytes.Contains(reentry, forbidden) {
			t.Fatalf("local re-entry request leaked settled search %q: %s", forbidden, reentry)
		}
	}
	checkpoint, ok, err = store.Get(context.Background(), workspace.Slug().String(), "swobu_turn2")
	if err != nil || !ok {
		t.Fatalf("turn 2 checkpoint after re-entry = (%t, %v)", ok, err)
	}
	settledAfterReentry := checkpoint.Response.Items()
	assertSettledWebSearchTruth(t, settledAfterReentry)
	if !reflect.DeepEqual(settledAfterReentry, settledBeforeReentry) {
		t.Fatalf("canonical settled lifecycle changed across re-entry:\n before %#v\n after  %#v", settledBeforeReentry, settledAfterReentry)
	}

	activeSearch := runCapabilityFallbackTurn(t, ingress, workspace, "turn4", `{
		"model":"chat",
		"previous_response_id":"swobu_turn3",
		"tools":[{"type":"web_search"}],
		"input":"search again"
	}`)
	if capabilityFallbackLocalTransportCount(transportCounts) != 2 || transportCounts["paid-openai"] != 2 {
		t.Fatalf("active-search continuation transports = %#v", transportCounts)
	}
	activeSearchChanges := activeSearch.Compatibility.Snapshot().Changes
	if len(activeSearchChanges) != 0 {
		t.Fatalf("failed local-candidate compatibility leaked into exact paid winner: %#v", activeSearchChanges)
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

func assertSettledWebSearchTruth(t *testing.T, items []canonical.CanonicalItem) {
	t.Helper()
	if len(items) != 3 || items[0].Kind() != canonical.ItemKindToolCall || items[1].Kind() != canonical.ItemKindToolResult || items[2].Kind() != canonical.ItemKindMessage {
		t.Fatalf("settled canonical web-search lifecycle = %#v", items)
	}
	call, _ := items[0].ToolCall()
	result, _ := items[1].ToolResult()
	if call.Tool().Kind() != canonical.ToolKindWebSearch {
		t.Fatalf("call kind = %q", call.Tool().Kind())
	}
	if _, ok := result.WebSearch(); !ok || result.CallID() != call.CallID() {
		t.Fatalf("result does not settle call %q", call.CallID())
	}
	searchCall, ok := call.Input().WebSearch()
	if !ok || !reflect.DeepEqual(searchCall.Queries, []string{"deadline"}) {
		t.Fatalf("search call = %#v", searchCall)
	}
	refinement, ok := call.ResponsesWebSearch()
	if !ok || refinement.ItemID().String() != "search_1" {
		t.Fatalf("responses refinement = (%#v, %t)", refinement, ok)
	}
	searchResult, _ := result.WebSearch()
	sources := searchResult.Sources()
	if len(sources) != 1 || sources[0].URL.String() != "https://example.test/rules" {
		t.Fatalf("search sources = %#v", sources)
	}
	if title, ok := sources[0].Title.Get(); !ok || title != "Rules" {
		t.Fatalf("search source title = (%q, %t)", title, ok)
	}
	message, _ := items[2].Message()
	content := message.Content()
	if len(content) != 1 {
		t.Fatalf("assistant content = %#v", content)
	}
	text, ok := content[0].Text()
	if !ok || text.Text() != "Hosted search answer" {
		t.Fatalf("assistant text = (%#v, %t)", text, ok)
	}
	citations := content[0].Citations()
	if len(citations) != 1 || citations[0].Source.URL.String() != "https://example.test/rules" {
		t.Fatalf("assistant citations = %#v", citations)
	}
	if title, ok := citations[0].Source.Title.Get(); !ok || title != "Rules" {
		t.Fatalf("citation title = (%q, %t)", title, ok)
	}
}
