package venice

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestVeniceLowersCanonicalWebSearchByToolPolicy(t *testing.T) {
	function := canonicaltest.FunctionTool(t, "lookup", canonicaltest.Schema(t, `{"type":"object"}`))
	tests := []struct {
		name       string
		tools      []canonical.ToolDeclaration
		policy     canonical.ToolPolicy
		wantMode   string
		wantSearch bool
		wantError  bool
	}{
		{name: "none", tools: []canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()}, policy: canonical.NewToolPolicy(canonical.ToolPolicyNone, nil)},
		{name: "auto", tools: []canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()}, policy: canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil), wantMode: "auto", wantSearch: true},
		{name: "required sole search", tools: []canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()}, policy: canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), wantMode: "on", wantSearch: true},
		{name: "required mixed tools", tools: []canonical.ToolDeclaration{canonical.NewWebSearchDeclaration(), function}, policy: canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil), wantError: true},
		{name: "specific sole search", tools: []canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()}, policy: canonical.NewToolPolicy(canonical.ToolPolicySpecific, webSearchKey()), wantMode: "on", wantSearch: true},
		{name: "specific search with function", tools: []canonical.ToolDeclaration{canonical.NewWebSearchDeclaration(), function}, policy: canonical.NewToolPolicy(canonical.ToolPolicySpecific, webSearchKey()), wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model:      canonical.Specify("venice-model"),
				Items:      []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, test.tools...), canonicaltest.Message(t, canonical.MessageRoleUser, "answer")},
				ToolPolicy: canonical.Specify(test.policy),
			})
			attempt := providerRequest(t, request, delivery.BufferedDelivery())
			document, _, err := resolvedCodec(t).Encode(attempt)
			if test.wantError {
				if err == nil {
					t.Fatal("expected widened search policy to fail")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			raw, present := payload["venice_parameters"]
			if present != test.wantSearch {
				t.Fatalf("venice_parameters present = %v, want %v: %s", present, test.wantSearch, document.RawBytes())
			}
			if test.wantSearch && !bytes.Contains(raw, []byte(`"enable_web_search":"`+test.wantMode+`"`)) {
				t.Fatalf("search mode missing: %s", raw)
			}
			if bytes.Contains(raw, []byte("include_search_results_in_stream")) {
				t.Fatalf("experimental search-results payload was requested: %s", raw)
			}
			if bytes.Contains(document.RawBytes(), []byte(`"type":"web_search"`)) {
				t.Fatalf("Venice search leaked as a Chat tool fragment: %s", document.RawBytes())
			}
		})
	}
}

func TestVeniceRaisesBufferedCitationsAndReasoning(t *testing.T) {
	request := baseRequest(t)
	attempt := providerRequest(t, request, delivery.BufferedDelivery())
	raw := []byte(`{"id":"chatcmpl_1","model":"venice-model","choices":[{"message":{"role":"assistant","reasoning_content":"check sources","content":"answer[REF]0[/REF]"},"finish_reason":"stop"}],"venice_parameters":{"web_search_citations":[{"title":"Source","url":"https://example.com/source"}]}}`)
	decoded, err := resolvedCodec(t).Decode(context.Background(), attempt, provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, raw, carrier.Meta{})})
	if err != nil {
		t.Fatal(err)
	}
	response := project(t, decoded.Stream)
	assertVeniceItems(t, response.Items())
}

func TestVeniceRaisesZeroBasedCitationsWithoutReordering(t *testing.T) {
	attempt := providerRequest(t, baseRequest(t), delivery.BufferedDelivery())
	raw := []byte(`{"id":"chatcmpl_1","model":"venice-model","choices":[{"message":{"role":"assistant","content":"answer[REF]0[/REF][REF]2[/REF]"},"finish_reason":"stop"}],"web_search_citations":[{"title":"Repeated","url":"https://example.com/repeated"},{"title":"Middle","url":"https://example.com/middle"},{"title":"Repeated","url":"https://example.com/repeated"}]}`)
	decoded, err := resolvedCodec(t).Decode(context.Background(), attempt, provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, raw, carrier.Meta{})})
	if err != nil {
		t.Fatal(err)
	}
	items := project(t, decoded.Stream).Items()
	result, ok := items[1].ToolResult()
	if !ok {
		t.Fatalf("search result = %#v", items[1])
	}
	search, ok := result.WebSearch()
	if !ok || len(search.Sources()) != 3 {
		t.Fatalf("ordered search sources = %#v", search.Sources())
	}
	message, ok := items[2].Message()
	if !ok {
		t.Fatalf("message = %#v", items[2])
	}
	part := message.Content()[0]
	text, _ := part.Text()
	if text.Text() != "answer" {
		t.Fatalf("message text = %q", text.Text())
	}
	citations := part.Citations()
	if len(citations) != 2 || citations[0].Source.URL.String() != "https://example.com/repeated" || citations[1].Source.URL.String() != "https://example.com/repeated" {
		t.Fatalf("message citations = %#v", citations)
	}
}

func TestVeniceRaisesLegacyCaretCitations(t *testing.T) {
	attempt := providerRequest(t, baseRequest(t), delivery.BufferedDelivery())
	raw := []byte(`{"id":"chatcmpl_1","model":"venice-model","choices":[{"message":{"role":"assistant","content":"answer^1,3^"},"finish_reason":"stop"}],"web_search_citations":[{"title":"First","url":"https://example.com/first"},{"title":"Middle","url":"https://example.com/middle"},{"title":"Third","url":"https://example.com/third"}]}`)
	decoded, err := resolvedCodec(t).Decode(context.Background(), attempt, provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, raw, carrier.Meta{})})
	if err != nil {
		t.Fatal(err)
	}
	items := project(t, decoded.Stream).Items()
	message, ok := items[2].Message()
	if !ok {
		t.Fatalf("message = %#v", items[2])
	}
	part := message.Content()[0]
	text, _ := part.Text()
	if text.Text() != "answer" {
		t.Fatalf("message text = %q", text.Text())
	}
	citations := part.Citations()
	if len(citations) != 2 || citations[0].Source.URL.String() != "https://example.com/first" || citations[1].Source.URL.String() != "https://example.com/third" {
		t.Fatalf("message citations = %#v", citations)
	}
}

func TestVeniceRaisesStreamedCitationsAndReasoning(t *testing.T) {
	request := baseRequest(t)
	attempt := providerRequest(t, request, delivery.StreamingDelivery(delivery.FramingSSE))
	sse := "data: {\"id\":\"chatcmpl_1\",\"model\":\"venice-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"check \"}}],\"venice_parameters\":{\"web_search_citations\":[{\"title\":\"Source\",\"url\":\"https://example.com/source\"}]}}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"model\":\"venice-model\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"sources\",\"content\":\"answer[REF]0[/REF]\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	decoded, err := resolvedCodec(t).Decode(context.Background(), attempt, provider.StreamIngress{Stream: carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(sse))}})
	if err != nil {
		t.Fatal(err)
	}
	response := project(t, decoded.Stream)
	assertVeniceItems(t, response.Items())
}

func TestVenicePreservesLiteralCitationSyntaxWithoutSearchBuffered(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("venice-model"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "ordinary request")},
	})
	attempt := providerRequest(t, request, delivery.BufferedDelivery())
	raw := []byte(`{"id":"chatcmpl_1","model":"venice-model","choices":[{"message":{"role":"assistant","content":"x[REF]0[/REF]"},"finish_reason":"stop"}]}`)
	decoded, err := resolvedCodec(t).Decode(context.Background(), attempt, provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, raw, carrier.Meta{})})
	if err != nil {
		t.Fatal(err)
	}
	assertSingleMessageText(t, project(t, decoded.Stream).Items(), "x[REF]0[/REF]")
}

func TestVenicePreservesLiteralCitationSyntaxWithoutSearchStreaming(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("venice-model"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "ordinary request")},
	})
	attempt := providerRequest(t, request, delivery.StreamingDelivery(delivery.FramingSSE))
	sse := "data: {\"id\":\"chatcmpl_1\",\"model\":\"venice-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"x[REF]0[/REF]\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	decoded, err := resolvedCodec(t).Decode(context.Background(), attempt, provider.StreamIngress{Stream: carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(sse))}})
	if err != nil {
		t.Fatal(err)
	}
	assertSingleMessageText(t, project(t, decoded.Stream).Items(), "x[REF]0[/REF]")
}

func TestVeniceDiscoveryPublishesExactTargetSupport(t *testing.T) {
	store := &modelSupportStore{byModel: make(map[string]provider.TargetSupport)}
	rows, err := modelcatalogopenai.DecodeModelRows(strings.NewReader(`{"data":[{"id":"capable","type":"text","owned_by":"venice.ai","model_spec":{"name":"Capable","capabilities":{"supportsFunctionCalling":false,"supportsReasoning":true,"supportsReasoningEffort":false,"supportsResponseSchema":true,"supportsWebSearch":true,"supportsVision":false}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	option, include, err := store.projectModel(profile.ProviderSpecVenice, rows[0])
	if err != nil || !include {
		t.Fatalf("project model: include=%v err=%v", include, err)
	}
	if option.DefaultProviderProtocol != "chat_completions" {
		t.Fatalf("default protocol = %q", option.DefaultProviderProtocol)
	}
	target := veniceTarget(delivery.BufferedDelivery())
	target.Model = "capable"
	support := store.ResolveTargetSupport(target)
	for _, capability := range []canonical.CapabilityPath{canonical.RequestTools, canonical.RequestReasoning, canonical.RequestOutputFormatSchema} {
		if got := support.Get(capability); got != provider.SupportSupported {
			t.Fatalf("%s support = %v, want supported", capability, got)
		}
	}
	for _, capability := range []canonical.CapabilityPath{canonical.RequestControlsEffort, canonical.RequestItemsMessageImage} {
		if got := support.Get(capability); got != provider.SupportUnsupported {
			t.Fatalf("%s support = %v, want unsupported", capability, got)
		}
	}
	if got := support.Get(canonical.ResponseItemsMessageCitations); got != provider.SupportUnknown {
		t.Fatalf("citation support = %v, want unknown", got)
	}
	if got := support.Get(canonical.RequestToolsKind); got != provider.SupportUnknown {
		t.Fatalf("tool-kind support = %v, want unknown", got)
	}
	if got := store.resolveWebSearch("capable"); got != provider.SupportSupported {
		t.Fatalf("Venice web-search support = %v, want supported", got)
	}
}

func TestVeniceRejectsSearchForModelKnownUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"no-search","type":"text","model_spec":{"capabilities":{"supportsFunctionCalling":true,"supportsWebSearch":false}}}]}`))
	}))
	defer server.Close()
	bundle := NewRuntime(server.Client(), veniceCredentialResolver{})
	target := provider.NewTargetSnapshot("draft", string(profile.ProviderSpecVenice), server.URL+"/api/v1", "env:VENICE_API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	target.Model = "no-search"
	if _, err := bundle.Discovery.ProbeTarget(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = backend.Codec.Encode(providerRequest(t, baseRequest(t), delivery.BufferedDelivery()))
	if err == nil {
		t.Fatal("expected known unsupported Venice web search to fail")
	}
}

func TestVeniceDiscoveryRequestsAndDefensivelyKeepsOnlyTextModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" || r.URL.Query().Get("type") != "text" {
			t.Fatalf("catalog request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"text-model","type":"text"},{"id":"image-model","type":"image"}]}`))
	}))
	defer server.Close()
	bundle := NewRuntime(server.Client(), veniceCredentialResolver{})
	probe, err := bundle.Discovery.ProbeTarget(context.Background(), provider.NewTargetSnapshot("draft", string(profile.ProviderSpecVenice), server.URL+"/api/v1", "env:VENICE_API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery()))
	if err != nil {
		t.Fatal(err)
	}
	models := probe.Options
	if len(models) != 1 || models[0].Name != "text-model" {
		t.Fatalf("models = %#v", models)
	}
}

func TestVeniceRaisesSearchLifecycleWithoutDisclosedSources(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify("venice-model"),
		Items:      []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, canonical.NewWebSearchDeclaration()), canonicaltest.Message(t, canonical.MessageRoleUser, "search")},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
	})
	attempt := providerRequest(t, request, delivery.BufferedDelivery())
	raw := []byte(`{"id":"chatcmpl_1","model":"venice-model","choices":[{"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}]}`)
	decoded, err := resolvedCodec(t).Decode(context.Background(), attempt, provider.DocumentIngress{Document: carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, raw, carrier.Meta{})})
	if err != nil {
		t.Fatal(err)
	}
	items := project(t, decoded.Stream).Items()
	if len(items) != 3 {
		t.Fatalf("items = %#v, want search call, empty result, message", items)
	}
	result, ok := items[1].ToolResult()
	if !ok {
		t.Fatalf("search result = %#v", items[1])
	}
	search, ok := result.WebSearch()
	if !ok || len(search.Sources()) != 0 {
		t.Fatalf("search sources = %#v", search.Sources())
	}
}

func TestVeniceSSEBodyYieldsFirstEventBeforeUpstreamEOF(t *testing.T) {
	reader, writer := io.Pipe()
	body := newVeniceSSEBody(context.Background(), reader, newCitationState())
	written := make(chan struct{})
	go func() {
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n")
		close(written)
	}()
	buffer := make([]byte, 256)
	count, err := body.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	<-written
	if !strings.Contains(string(buffer[:count]), `"content":"first"`) {
		t.Fatalf("first read = %q", buffer[:count])
	}
	_ = writer.Close()
	_ = body.Close()
}

func TestVeniceSSEBodyRemovesCitationMarkerAcrossEvents(t *testing.T) {
	state := newCitationState()
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"answer[RE\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"F]0[/REF][REF]2[/R\"}}],\"web_search_citations\":[{\"title\":\"First\",\"url\":\"https://example.com/same\"},{\"title\":\"Second\",\"url\":\"https://example.com/second\"},{\"title\":\"First\",\"url\":\"https://example.com/same\"}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"EF]\"}}]}\n\n" +
		"data: [DONE]\n\n"
	body := newVeniceSSEBody(context.Background(), io.NopCloser(strings.NewReader(sse)), state)
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "[REF]") || strings.Contains(string(raw), "[/R") {
		t.Fatalf("citation marker leaked: %s", raw)
	}
	citations, references := state.snapshot()
	if len(citations) != 3 {
		t.Fatalf("citations = %#v", citations)
	}
	if _, first := references[0]; !first {
		t.Fatalf("references = %#v", references)
	}
	if _, third := references[2]; !third {
		t.Fatalf("references = %#v", references)
	}
}

func TestVeniceSSEBodyRemovesLegacyCaretMarkerAcrossEvents(t *testing.T) {
	state := newCitationState()
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"answer^1,\"}}],\"web_search_citations\":[{\"title\":\"First\",\"url\":\"https://example.com/first\"},{\"title\":\"Middle\",\"url\":\"https://example.com/middle\"},{\"title\":\"Third\",\"url\":\"https://example.com/third\"}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"3^\"}}]}\n\n" +
		"data: [DONE]\n\n"
	body := newVeniceSSEBody(context.Background(), io.NopCloser(strings.NewReader(sse)), state)
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "^1,3^") {
		t.Fatalf("legacy citation marker leaked: %s", raw)
	}
	_, references := state.snapshot()
	if _, first := references[0]; !first {
		t.Fatalf("references = %#v", references)
	}
	if _, third := references[2]; !third {
		t.Fatalf("references = %#v", references)
	}
}

func TestVeniceSSEBodyUsesDecodeContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	body := newVeniceSSEBody(ctx, reader, newCitationState())
	cancel()
	_, err := body.Read(make([]byte, 16))
	if err != context.Canceled {
		t.Fatalf("read error = %v, want context canceled", err)
	}
	_ = writer.Close()
}

type veniceCredentialResolver struct{}

func (veniceCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "venice-token", nil
}

func baseRequest(t *testing.T) canonical.CanonicalRequest {
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("venice-model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, canonical.NewWebSearchDeclaration()), canonicaltest.Message(t, canonical.MessageRoleUser, "search")},
	})
}

func providerRequest(t *testing.T, request canonical.CanonicalRequest, responseDelivery delivery.Delivery) provider.Request {
	t.Helper()
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	return provider.Request{ExchangeID: "ex_venice", Canonical: request, ToolNames: names, Delivery: responseDelivery}
}

func resolvedCodec(t *testing.T) provider.Codec {
	t.Helper()
	backend, err := NewRuntime(http.DefaultClient, nil).BackendResolver.ResolveBackend(veniceTarget(delivery.BufferedDelivery()))
	if err != nil {
		t.Fatal(err)
	}
	return backend.Codec
}

func veniceTarget(providerDelivery delivery.Delivery) provider.TargetSnapshot {
	protocol := "chat_completions"
	if providerDelivery.IsStreaming() {
		protocol = "chat_completions_stream"
	}
	target := provider.NewTargetSnapshot("venice", string(profile.ProviderSpecVenice), "https://example.test/api/v1", "env:VENICE_API_KEY", protocolkind.ChatCompletions, protocol, providerDelivery)
	target.Model = "venice-model"
	return target
}

func webSearchKey() *canonical.ToolKey {
	key := canonical.NewWebSearchDeclaration().Key()
	return &key
}

func project(t *testing.T, stream canonical.ResponseStream) canonical.CanonicalResponse {
	t.Helper()
	binding := canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("resp_venice"), TargetID: "venice", TargetVersion: 1}
	response, err := canonical.ProjectStream(context.Background(), canonical.NewBoundResponseIdentityStream(stream, binding), binding)
	if err != nil {
		t.Fatal(err)
	}
	return *response
}

func assertVeniceItems(t *testing.T, items []canonical.CanonicalItem) {
	t.Helper()
	if len(items) != 4 {
		t.Fatalf("items = %#v, want search call, result, reasoning, message", items)
	}
	call, ok := items[0].ToolCall()
	if !ok || call.Tool().Kind() != canonical.ToolKindWebSearch {
		t.Fatalf("search call = %#v", items[0])
	}
	result, ok := items[1].ToolResult()
	if !ok || result.CallID() != call.CallID() {
		t.Fatalf("search result = %#v", items[1])
	}
	search, ok := result.WebSearch()
	if !ok || len(search.Sources()) != 1 || search.Sources()[0].URL.String() != "https://example.com/source" {
		t.Fatalf("search sources = %#v", search.Sources())
	}
	reasoning, ok := items[2].Reasoning()
	if !ok || len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Text() != "check sources" {
		t.Fatalf("reasoning = %#v", items[2])
	}
	message, ok := items[3].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("message = %#v", items[3])
	}
	text, ok := message.Content()[0].Text()
	if !ok || text.Text() != "answer" {
		t.Fatalf("message text = %#v", message.Content())
	}
	if citations := message.Content()[0].Citations(); len(citations) != 1 || citations[0].Source.URL.String() != "https://example.com/source" {
		t.Fatalf("message citations = %#v", citations)
	}
}

func assertSingleMessageText(t *testing.T, items []canonical.CanonicalItem, want string) {
	t.Helper()
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one message", items)
	}
	message, ok := items[0].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("message = %#v", items[0])
	}
	text, ok := message.Content()[0].Text()
	if !ok || text.Text() != want {
		t.Fatalf("message text = %#v, want %q", message.Content(), want)
	}
}
