package openaifamily

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type commodityCredentialResolver struct{}

func (commodityCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "commodity-token", nil
}

type countingCredentialResolver struct {
	calls int
}

func (r *countingCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	r.calls++
	return "commodity-token", nil
}

func TestOVHCloudSharedRuntimeSupportsAnonymousAndBearerStreamingChat(t *testing.T) {
	for _, tc := range []struct {
		name           string
		credentialRef  string
		wantAuth       string
		wantResolution int
	}{
		{name: "anonymous"},
		{name: "authenticated", credentialRef: "env:OVH_AI_ENDPOINTS_ACCESS_TOKEN", wantAuth: "Bearer commodity-token", wantResolution: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &countingCredentialResolver{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
					t.Fatalf("request = %s %s", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != tc.wantAuth {
					t.Fatalf("Authorization = %q, want %q", got, tc.wantAuth)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload["stream"] != true || payload["parallel_tool_calls"] != false {
					t.Fatalf("stream/tool controls = %#v", payload)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_ovh\",\"model\":\"exact-model\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()

			bundle := NewRuntime(server.Client(), resolver, StandardBearerPolicy(profile.ProviderSpecOVHCloud))
			target := provider.NewTargetSnapshot("ovhcloud", "ovhcloud", server.URL+"/v1", tc.credentialRef, protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
			target.Model = "exact-model"
			backend, err := bundle.BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify(target.Model),
				Items: []canonical.CanonicalItem{
					canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())),
					canonicaltest.Message(t, canonical.MessageRoleUser, "hello"),
				},
				ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
			})
			toolNames, _, err := provider.BuildAttemptToolNames(request)
			if err != nil {
				t.Fatal(err)
			}
			document, changes, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: toolNames, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			if len(changes) != 0 {
				t.Fatalf("OVHcloud request changes = %#v", changes)
			}
			ingress, err := backend.Transport.Send(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := backend.Codec.Decode(context.Background(), provider.Request{ExchangeID: "ex_ovh", Canonical: request}, ingress)
			if err != nil {
				t.Fatal(err)
			}
			for {
				_, nextErr := decoded.Stream.Next(context.Background())
				if nextErr == io.EOF {
					break
				}
				if nextErr != nil {
					t.Fatal(nextErr)
				}
			}
			if err := decoded.Stream.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if resolver.calls != tc.wantResolution {
				t.Fatalf("credential resolutions = %d, want %d", resolver.calls, tc.wantResolution)
			}
		})
	}
}

func TestOVHCloudSharedRuntimeDecodesFragmentedToolCallAndReplaysResult(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if requestCount == 1 {
			_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_tool\",\"model\":\"exact-model\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"function\",\"id\":\"call_lookup\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\"}}]}}]}\n\n"+
				"data: {\"id\":\"chatcmpl_tool\",\"model\":\"exact-model\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"x\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		wireMessages := string(payload["messages"])
		if !strings.Contains(wireMessages, `"tool_call_id":"call_lookup"`) || !strings.Contains(wireMessages, `"content":"found"`) {
			t.Fatalf("second request did not replay tool result: %s", wireMessages)
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_final\",\"model\":\"exact-model\",\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	bundle := NewRuntime(server.Client(), &countingCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecOVHCloud))
	target := provider.NewTargetSnapshot("ovhcloud", "ovhcloud", server.URL+"/v1", "", protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = "exact-model"
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	base := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{
		canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object","properties":{"q":{"type":"string"}}}`), canonical.Unspecified[bool]())),
		canonicaltest.Message(t, canonical.MessageRoleUser, "look it up"),
	}})
	toolNames, _, err := provider.BuildAttemptToolNames(base)
	if err != nil {
		t.Fatal(err)
	}
	first := provider.Request{ExchangeID: "ex_ovh_tool_1", Canonical: base, ToolNames: toolNames, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}
	document, _, err := backend.Codec.Encode(first)
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := backend.Transport.Send(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.Codec.Decode(context.Background(), first, ingress)
	if err != nil {
		t.Fatal(err)
	}
	firstBinding := canonical.ResponseBinding{SwobuID: "resp_ovh_tool_1", TargetID: target.TargetID, TargetVersion: target.TargetVersion}
	response, err := canonical.ProjectStream(context.Background(), canonical.NewBoundResponseIdentityStream(decoded.Stream, firstBinding), firstBinding)
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 1 {
		t.Fatalf("first response items = %#v", items)
	}
	call, ok := items[0].ToolCall()
	if !ok || call.CallID().String() != "call_lookup" || call.Tool() != key {
		t.Fatalf("fragmented tool call = %#v", items[0])
	}
	result, err := canonical.NewToolResultItem(call.CallID(), []canonical.ToolResultPart{canonical.NewTextToolResultPart("found")}, false)
	if err != nil {
		t.Fatal(err)
	}
	secondCanonical := base.WithItems(append(base.Items(), items[0], result))
	secondNames, _, err := provider.BuildAttemptToolNames(secondCanonical)
	if err != nil {
		t.Fatal(err)
	}
	second := provider.Request{ExchangeID: "ex_ovh_tool_2", Canonical: secondCanonical, ToolNames: secondNames, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}
	document, _, err = backend.Codec.Encode(second)
	if err != nil {
		t.Fatal(err)
	}
	ingress, err = backend.Transport.Send(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = backend.Codec.Decode(context.Background(), second, ingress)
	if err != nil {
		t.Fatal(err)
	}
	secondBinding := canonical.ResponseBinding{SwobuID: "resp_ovh_tool_2", TargetID: target.TargetID, TargetVersion: target.TargetVersion}
	finalResponse, err := canonical.ProjectStream(context.Background(), canonical.NewBoundResponseIdentityStream(decoded.Stream, secondBinding), secondBinding)
	if err != nil {
		t.Fatal(err)
	}
	finalItems := finalResponse.Items()
	if len(finalItems) != 1 {
		t.Fatalf("final response items = %#v", finalItems)
	}
	message, ok := finalItems[0].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("final response message = %#v", finalItems[0])
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
}

func TestBasetenAndHyperbolicProfilesPreserveTheirDifferentAuthoringContracts(t *testing.T) {
	baseten, ok := profile.ProfileForSpec(string(profile.ProviderSpecBaseten))
	if !ok {
		t.Fatal("Baseten profile is missing")
	}
	if baseten.ProviderDisplayName != "Baseten" || baseten.Locator.Kind != profile.LocatorBaseURL || baseten.Locator.Default != "https://inference.baseten.co/v1" || baseten.ModelDiscovery != profile.ModelDiscoveryModeAdvisory {
		t.Fatalf("Baseten profile = %#v", baseten)
	}
	if got := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecBaseten)); !reflect.DeepEqual(got, []string{"chat_completions_stream", "messages_stream"}) {
		t.Fatalf("Baseten protocols = %v", got)
	}

	hyperbolic, ok := profile.ProfileForSpec(string(profile.ProviderSpecHyperbolic))
	if !ok {
		t.Fatal("Hyperbolic profile is missing")
	}
	if hyperbolic.ProviderDisplayName != "Hyperbolic" || hyperbolic.Locator.Kind != profile.LocatorFixed || hyperbolic.Locator.Default != "https://api.hyperbolic.xyz/v1" || hyperbolic.ModelDiscovery != profile.ModelDiscoveryModeNone {
		t.Fatalf("Hyperbolic profile = %#v", hyperbolic)
	}
	if got := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecHyperbolic)); !reflect.DeepEqual(got, []string{"chat_completions_stream"}) {
		t.Fatalf("Hyperbolic protocols = %v", got)
	}
}

func TestBasetenSharedRuntimeSupportsExactOverrideChatAndMessagesWithoutCatalogSubstitution(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer commodity-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("User-Agent") != "swobu/dev" {
			t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/deployment/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/deployment/v1/chat/completions":
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if string(payload["model"]) != `"served/exact"` || len(payload["messages"]) == 0 {
				t.Fatalf("Chat payload = %#v", payload)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		case "/deployment/v1/messages":
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if string(payload["model"]) != `"served/exact"` || len(payload["messages"]) == 0 {
				t.Fatalf("Messages payload = %#v", payload)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"served/exact\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"+
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"+
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":1}}\n\n"+
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	bundle := NewRuntime(server.Client(), commodityCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecBaseten))
	baseURL := server.URL + "/deployment/v1"
	probeTarget := provider.NewTargetSnapshot("baseten", "baseten", baseURL, "env:BASETEN_API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	probe, err := bundle.Discovery.ProbeTarget(context.Background(), probeTarget)
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.Options) != 0 {
		t.Fatalf("overridden deployment catalog = %#v, want empty catalog", probe.Options)
	}
	for _, tc := range []struct {
		name     string
		kind     protocolkind.ProtocolKind
		protocol string
	}{
		{name: "chat", kind: protocolkind.ChatCompletions, protocol: "chat_completions"},
		{name: "messages", kind: protocolkind.Messages, protocol: "messages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := provider.NewTargetSnapshot("baseten", "baseten", baseURL, "env:BASETEN_API_KEY", tc.kind, tc.protocol, delivery.BufferedDelivery())
			target.Model = "served/exact"
			backend, err := bundle.BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Transport.Send(context.Background(), document); err != nil {
				t.Fatal(err)
			}
		})
	}
	if !reflect.DeepEqual(paths, []string{"/deployment/v1/models", "/deployment/v1/chat/completions", "/deployment/v1/messages"}) {
		t.Fatalf("paths = %v", paths)
	}
}

func TestCommodityChatTargetsBridgeResponsesAndMessagesIngress(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resolver provider.BackendResolver
	}{
		{name: "baseten", resolver: NewRuntime(nil, commodityCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecBaseten)).BackendResolver},
		{name: "hyperbolic", resolver: NewRuntime(nil, commodityCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecHyperbolic)).BackendResolver},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := provider.NewTargetSnapshot(tc.name, tc.name, "https://example.test/v1", "env:API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
			target.Model = "provider/exact"
			backend, err := tc.resolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			req := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("provider/exact"),
				Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
			})
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: req, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if string(payload["model"]) != `"provider/exact"` || len(payload["messages"]) == 0 || string(payload["stream"]) != "true" {
				t.Fatalf("ingress Chat payload = %#v", payload)
			}
		})
	}
}

func TestHyperbolicManualRuntimeDoesNotProbeOrAddProtocolFamilies(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	bundle := NewRuntime(server.Client(), commodityCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecHyperbolic))
	target := provider.NewTargetSnapshot("hyperbolic", "hyperbolic", server.URL+"/v1", "env:HYPERBOLIC_API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	target.Model = "provider/exact-model"
	result, err := bundle.Discovery.ProbeTarget(context.Background(), target)
	if err == nil || len(result.Options) != 0 || called {
		t.Fatalf("manual discovery = %#v, err=%v, called=%v", result, err, called)
	}
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.Responses, protocolkind.Messages} {
		invalid := target.Clone()
		invalid.ProtocolKind = kind
		if _, err := bundle.BackendResolver.ResolveBackend(invalid); err == nil {
			t.Fatalf("Hyperbolic resolved unsupported protocol %s", kind)
		}
	}
}

func TestCommodityProvidersDispatchUnsupportedOptionalCombination(t *testing.T) {
	dispatched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatched = true
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"selected model rejects this combination"}}`))
	}))
	defer server.Close()
	bundle := NewRuntime(server.Client(), commodityCredentialResolver{}, StandardBearerPolicy(profile.ProviderSpecHyperbolic))
	target := provider.NewTargetSnapshot("hyperbolic", "hyperbolic", server.URL+"/v1", "env:HYPERBOLIC_API_KEY", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	target.Model = "unknown/exact-model"
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONObject})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), OutputFormat: canonical.Specify(format), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "json")}})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err == nil {
		t.Fatal("provider rejection was not returned")
	}
	if !dispatched {
		t.Fatal("optional combination never reached the provider")
	}
}
