package modelscope

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{ calls int }

func (r *credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	r.calls++
	return "ms-token", nil
}

func TestReasoningContentBecomesReadableTraceWithoutReplayState(t *testing.T) {
	document := carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(`{"choices":[{"message":{"role":"assistant","reasoning_content":"inspect first","content":"answer"},"finish_reason":"stop"}]}`), carrier.Meta{})
	cleaned, item, err := protocolcodec.ExtractChatReasoningDocument(document, protocolcodec.ReasoningContentExtractor{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned.RawBytes(), []byte("reasoning_content")) {
		t.Fatalf("reasoning carrier leaked: %s", cleaned.RawBytes())
	}
	assertReadableReasoning(t, item, "inspect first")

	stream := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"inspect \"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"repository\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"
	body := protocolcodec.NewChatReasoningSSEBody(io.NopCloser(strings.NewReader(stream)), protocolcodec.ReasoningContentExtractor{})
	cleanedStream, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleanedStream, []byte("reasoning_content")) || !bytes.Contains(cleanedStream, []byte("tool_calls")) {
		t.Fatalf("cleaned stream = %s", cleanedStream)
	}
	item, ok := body.Take()
	if !ok {
		t.Fatal("streamed reasoning item missing")
	}
	assertReadableReasoning(t, item, "inspect repository")
}

func TestMalformedReasoningContentFailsAtProviderDecoder(t *testing.T) {
	for _, test := range []struct {
		name     string
		buffered bool
	}{
		{name: "buffered", buffered: true},
		{name: "streamed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.buffered {
				message := map[string]json.RawMessage{"reasoning_content": json.RawMessage(`42`)}
				if _, err := (protocolcodec.ReasoningContentExtractor{}).ExtractBufferedChatReasoning(message); err == nil {
					t.Fatal("non-string buffered reasoning_content decoded")
				}
				return
			}
			delta := map[string]json.RawMessage{"reasoning_content": json.RawMessage(`42`)}
			if _, err := (protocolcodec.ReasoningContentExtractor{}).ExtractStreamedChatReasoning(delta); err == nil {
				t.Fatal("non-string streamed reasoning_content decoded")
			}
		})
	}
}

func TestRuntimeUsesBearerStandardChatAndPreservesOpaqueCatalogIDs(t *testing.T) {
	resolver := &credentialResolver{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ms-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"Qwen/Qwen3-Coder-30B-A3B-Instruct"},{"id":"ZhipuAI/GLM-5.1:DashScope"}]}`)
		case "/v1/chat/completions":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["model"] != "ZhipuAI/GLM-5.1:DashScope" || payload["stream"] != true {
				t.Fatalf("standard payload = %#v", payload)
			}
			for _, forbidden := range []string{"enable_thinking", "thinking_budget", "chat_template_kwargs"} {
				if _, present := payload[forbidden]; present {
					t.Fatalf("ModelScope request dialect %q leaked: %#v", forbidden, payload)
				}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	bundle := NewRuntime(server.Client(), resolver)
	target := modelScopeTarget(server.URL+"/v1", "ZhipuAI/GLM-5.1:DashScope")
	result, err := bundle.Discovery.ProbeTarget(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Options) != 2 || result.Options[0].Name != "Qwen/Qwen3-Coder-30B-A3B-Instruct" || result.Options[1].Name != "ZhipuAI/GLM-5.1:DashScope" {
		t.Fatalf("catalog options = %#v", result.Options)
	}
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
	document, changes, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil || len(changes) != 0 {
		t.Fatalf("encode = %v, changes %#v", err, changes)
	}
	ingress, err := backend.Transport.Send(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.Codec.Decode(context.Background(), provider.Request{Attempt: provider.AttemptContext{ExchangeID: "ex_modelscope"}, Canonical: request}, ingress)
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
	if resolver.calls != 2 {
		t.Fatalf("credential resolutions = %d, want discovery and chat", resolver.calls)
	}
}

func TestRuntimeRejectsNonChatProtocol(t *testing.T) {
	target := provider.NewTargetSnapshot("modelscope", string(profile.ProviderSpecModelScope), "https://example.test/v1", "env:MODELSCOPE_TOKEN", protocolkind.Responses, "responses_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	if _, err := NewRuntime(http.DefaultClient, &credentialResolver{}).BackendResolver.ResolveBackend(target); err == nil {
		t.Fatal("Responses resolved for ModelScope")
	}
}

func TestRequiredCredentialFailsBeforeDispatch(t *testing.T) {
	dispatched := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { dispatched = true }))
	defer server.Close()
	target := modelScopeTarget(server.URL+"/v1", "Qwen/Qwen3-Coder-30B-A3B-Instruct")
	target.CredentialRef = ""
	backend, err := NewRuntime(server.Client(), &credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err == nil {
		t.Fatal("ModelScope request without required credential was dispatched")
	}
	if dispatched {
		t.Fatal("ModelScope missing-credential failure reached HTTP backend")
	}
}

func TestReasoningFragmentedToolCallAndResultCompleteTwoRequestLoop(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if requestCount == 1 {
			_, _ = io.WriteString(w, "data: {\"id\":\"chat_tool\",\"model\":\"Qwen/Qwen3-Coder-30B-A3B-Instruct\",\"choices\":[{\"delta\":{\"reasoning_content\":\"inspect \"}}]}\n\n"+
				"data: {\"id\":\"chat_tool\",\"model\":\"Qwen/Qwen3-Coder-30B-A3B-Instruct\",\"choices\":[{\"delta\":{\"reasoning_content\":\"repository\"}}]}\n\n"+
				"data: {\"id\":\"chat_tool\",\"model\":\"Qwen/Qwen3-Coder-30B-A3B-Instruct\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_lookup\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\"}}]}}]}\n\n"+
				"data: {\"id\":\"chat_tool\",\"model\":\"Qwen/Qwen3-Coder-30B-A3B-Instruct\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"x\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		messages := string(payload["messages"])
		if !strings.Contains(messages, `"tool_call_id":"call_lookup"`) || !strings.Contains(messages, `"content":"found"`) || strings.Contains(messages, "reasoning_content") {
			t.Fatalf("second request history = %s", messages)
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"chat_final\",\"model\":\"Qwen/Qwen3-Coder-30B-A3B-Instruct\",\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	bundle := NewRuntime(server.Client(), &credentialResolver{})
	target := modelScopeTarget(server.URL, "Qwen/Qwen3-Coder-30B-A3B-Instruct")
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	base := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{
		canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object","properties":{"q":{"type":"string"}}}`), canonical.Unspecified[bool]())),
		canonicaltest.Message(t, canonical.MessageRoleUser, "look it up"),
	}})
	firstItems := sendAndProject(t, backend, target, base, "ex_modelscope_tool_1")
	if len(firstItems) != 2 {
		t.Fatalf("reasoning/tool items = %#v", firstItems)
	}
	assertReadableReasoning(t, firstItems[0], "inspect repository")
	call, ok := firstItems[1].ToolCall()
	if !ok || call.CallID().String() != "call_lookup" || call.Tool() != key {
		t.Fatalf("fragmented tool call = %#v", firstItems[1])
	}
	result, err := canonical.NewToolResultItem(call.CallID(), []canonical.ToolResultPart{canonical.NewTextToolResultPart("found")}, false)
	if err != nil {
		t.Fatal(err)
	}
	second := base.WithItems(append(base.Items(), firstItems[0], firstItems[1], result))
	finalItems := sendAndProject(t, backend, target, second, "ex_modelscope_tool_2")
	if len(finalItems) != 1 {
		t.Fatalf("final items = %#v", finalItems)
	}
	message, ok := finalItems[0].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("final message = %#v", finalItems[0])
	}
	text, ok := message.Content()[0].Text()
	if !ok || text.Text() != "done" {
		t.Fatalf("final message content = %#v", message.Content())
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d", requestCount)
	}
}

func sendAndProject(t *testing.T, backend provider.Backend, target provider.TargetSnapshot, request canonical.CanonicalRequest, exchangeID string) []canonical.CanonicalItem {
	t.Helper()
	toolNames, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	attempt := provider.Request{Attempt: provider.AttemptContext{ExchangeID: exchangeID}, Canonical: request, ToolNames: toolNames, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}
	document, _, err := backend.Codec.Encode(attempt)
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := backend.Transport.Send(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.Codec.Decode(context.Background(), attempt, ingress)
	if err != nil {
		t.Fatal(err)
	}
	binding := canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("resp_" + exchangeID), TargetID: target.TargetID, TargetVersion: target.TargetVersion}
	response, err := canonical.ProjectStream(context.Background(), canonical.NewBoundResponseIdentityStream(decoded.Stream, binding), binding)
	if err != nil {
		t.Fatal(err)
	}
	return response.Items()
}

func assertReadableReasoning(t *testing.T, item canonical.CanonicalItem, want string) {
	t.Helper()
	reasoning, ok := item.Reasoning()
	if !ok || len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Kind() != canonical.ReasoningPartTrace || reasoning.Parts()[0].Text() != want {
		t.Fatalf("reasoning item = %#v", item)
	}
	if !reasoning.Opaque().IsZero() {
		t.Fatalf("readable ModelScope reasoning acquired replay state: %#v", reasoning.Opaque())
	}
}

func modelScopeTarget(baseURL, model string) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("modelscope", string(profile.ProviderSpecModelScope), baseURL, "env:MODELSCOPE_TOKEN", protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = model
	return target
}
