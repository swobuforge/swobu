package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func TestChatReasoningResponseBecomesReadableCanonicalTrace(t *testing.T) {
	document := carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(`{"choices":[{"message":{"role":"assistant","reasoning":"think first","content":"answer"},"finish_reason":"stop"}]}`), carrier.Meta{})
	cleaned, item, err := protocolcodec.ExtractChatReasoningDocument(document, groqChatReasoningExtractor{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned.RawBytes(), []byte(`"reasoning"`)) {
		t.Fatalf("Groq reasoning leaked into shared Chat decode: %s", cleaned.RawBytes())
	}
	reasoning, ok := item.Reasoning()
	if !ok || len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Text() != "think first" {
		t.Fatalf("reasoning item = %#v", item)
	}

	stream := "data: {\"choices\":[{\"delta\":{\"reasoning\":\"think \"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"reasoning\":\"now\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"mark\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	body := protocolcodec.NewChatReasoningSSEBody(io.NopCloser(bytes.NewBufferString(stream)), groqChatReasoningExtractor{})
	cleanedStream, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleanedStream, []byte(`"reasoning"`)) || !bytes.Contains(cleanedStream, []byte(`"tool_calls"`)) {
		t.Fatalf("cleaned stream = %s", cleanedStream)
	}
	item, ok = body.Take()
	if !ok {
		t.Fatal("streamed Groq reasoning item missing")
	}
	reasoning, ok = item.Reasoning()
	if !ok || len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Text() != "think now" {
		t.Fatalf("streamed reasoning item = %#v", item)
	}
}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "groq-token", nil
}

func TestRuntimeAdmitsOnlyGroqChatAndResponses(t *testing.T) {
	bundle := NewRuntime(http.DefaultClient, credentialResolver{})
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses} {
		backend, err := bundle.BackendResolver.ResolveBackend(groqTarget("https://api.groq.com/openai/v1", kind))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if kind == protocolkind.ChatCompletions {
			if codec, ok := backend.Codec.(protocolcodec.Codec); !ok || codec.ChatDialect.ResponseReasoning == nil {
				t.Fatalf("Chat codec = %T", backend.Codec)
			}
		}
		if kind == protocolkind.Responses {
			if codec, ok := backend.Codec.(protocolcodec.Codec); !ok || codec.Protocol != protocolkind.Responses {
				t.Fatalf("Responses codec = %T", backend.Codec)
			}
		}
	}
	if _, err := bundle.BackendResolver.ResolveBackend(groqTarget("https://api.groq.com/openai/v1", protocolkind.Messages)); err == nil {
		t.Fatal("Messages resolved for Groq")
	}
}

func TestTransportAndDiscoveryUseResolvedBaseAndBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer groq-token" {
			t.Fatalf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/openai/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		case "/openai/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"future-model"}]}`))
		default:
			t.Fatalf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	bundle := NewRuntime(server.Client(), credentialResolver{})
	target := groqTarget(server.URL+"/openai/v1", protocolkind.ChatCompletions)
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("future-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
	})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err != nil {
		t.Fatal(err)
	}
	result, err := bundle.Discovery.ProbeTarget(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Options) != 1 || result.Options[0].Name != "future-model" {
		t.Fatalf("deployments = %#v", result.Options)
	}
}

func TestChatReasoningAndFlexUseOnlyEstablishedAttemptFacts(t *testing.T) {
	high := canonical.InferenceEffortHigh
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &high})
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Disclosure: canonical.Specify(canonical.ReasoningDisclosureNone),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("unclassified-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		Controls: controls, Reasoning: reasoning,
	})
	bundle := NewRuntime(nil, credentialResolver{})
	backend, err := bundle.BackendResolver.ResolveBackend(groqTarget("https://api.groq.com/openai/v1", protocolkind.ChatCompletions))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		next     bool
		wantTier bool
	}{
		{name: "fallback candidate", next: true, wantTier: true},
		{name: "terminal candidate", next: false, wantTier: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, _, err := backend.Codec.Encode(provider.Request{
				Canonical: request, Delivery: delivery.BufferedDelivery(), EncodeContext: provider.EncodeContext{HasNextRouteCandidate: test.next},
			})
			if err != nil {
				t.Fatal(err)
			}
			payload := groqPayload(t, document.RawBytes())
			if got := payload["reasoning_effort"]; got != "high" {
				t.Fatalf("reasoning_effort = %#v", got)
			}
			if got := payload["include_reasoning"]; got != false {
				t.Fatalf("include_reasoning = %#v", got)
			}
			gotTier, hasTier := payload["service_tier"]
			if hasTier != test.wantTier || test.wantTier && gotTier != "auto" {
				t.Fatalf("service_tier = %#v (present %t), want auto present %t", payload["service_tier"], hasTier, test.wantTier)
			}
			if test.wantTier != isMarkedServiceTierAuto(document) {
				t.Fatalf("service-tier marker = %t, want %t", isMarkedServiceTierAuto(document), test.wantTier)
			}
			if _, present := payload["reasoning_format"]; present {
				t.Fatalf("model-specific reasoning_format leaked: %s", document.RawBytes())
			}
		})
	}
}

func TestChatLeavesUnspecifiedGroqFieldsAbsent(t *testing.T) {
	bundle := NewRuntime(nil, credentialResolver{})
	backend, err := bundle.BackendResolver.ResolveBackend(groqTarget("https://api.groq.com/openai/v1", protocolkind.ChatCompletions))
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("any-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
	})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"reasoning_effort", "include_reasoning", "service_tier", "reasoning_format"} {
		if _, present := groqPayload(t, document.RawBytes())[name]; present {
			t.Fatalf("unspecified Groq field %q leaked: %s", name, document.RawBytes())
		}
	}
}

func TestChatOmitsUnsupportedOrdinalWithoutModelInference(t *testing.T) {
	bundle := NewRuntime(nil, credentialResolver{})
	backend, err := bundle.BackendResolver.ResolveBackend(groqTarget("https://api.groq.com/openai/v1", protocolkind.ChatCompletions))
	if err != nil {
		t.Fatal(err)
	}
	maximum := canonical.InferenceEffortMax
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &maximum})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("any-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}, Controls: controls,
	})
	document, changes, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := groqPayload(t, document.RawBytes())["reasoning_effort"]; present {
		t.Fatalf("unsupported ordinal acquired a provider spelling: %s", document.RawBytes())
	}
	want := compat.NewOmission(canonical.RequestControlsEffort, canonical.Occurrence{})
	if len(changes) != 1 || changes[0] != want {
		t.Fatalf("changes = %#v, want %#v", changes, want)
	}
}

func TestResponsesStayStatelessAndDoNotUseFlexCarrier(t *testing.T) {
	backend, err := NewRuntime(http.DefaultClient, credentialResolver{}).BackendResolver.ResolveBackend(groqTarget("https://api.groq.com/openai/v1", protocolkind.Responses))
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("future-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn two")},
	})
	document, _, err := backend.Codec.Encode(provider.Request{
		Canonical: request, Delivery: delivery.BufferedDelivery(),
		PreviousHistory: &provider.PreviousHistory{Response: canonical.ResponseRef{Responses: &canonical.ResponsesContinuation{ProviderResponseID: "never-send", TargetID: "target", TargetVersion: 1}}, OmitStart: 0, OmitEnd: 1},
		EncodeContext:   provider.EncodeContext{HasNextRouteCandidate: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := groqPayload(t, document.RawBytes())
	if _, present := payload["previous_response_id"]; present {
		t.Fatalf("stateless Groq Responses carried a continuation: %s", document.RawBytes())
	}
	if _, present := payload["service_tier"]; present {
		t.Fatalf("Groq Responses carried Chat-only service tier: %s", document.RawBytes())
	}
}

func TestCapacityExceededIsPreExecutionOnlyForMarkedFlexDocument(t *testing.T) {
	capacity := canonical.NewBackendError("groq", 498, `{"error":{"code":"capacity_exceeded"}}`, "")
	for _, test := range []struct {
		name     string
		document string
		marked   bool
		failure  error
		want     provider.ExecutionPossibility
	}{
		{name: "marked documented capacity", document: `{"service_tier":"auto"}`, marked: true, failure: provider.AttemptMayHaveExecuted(capacity), want: provider.ExecutionRejectedBeforeExecution},
		{name: "unmarked capacity", document: `{"service_tier":"auto"}`, failure: provider.AttemptMayHaveExecuted(capacity), want: provider.ExecutionMayHaveOccurred},
		{name: "wrong carrier", document: `{}`, marked: true, failure: provider.AttemptMayHaveExecuted(capacity), want: provider.ExecutionMayHaveOccurred},
		{name: "other 498", document: `{"service_tier":"auto"}`, marked: true, failure: provider.AttemptMayHaveExecuted(canonical.NewBackendError("groq", 498, `{"error":{"code":"other"}}`, "")), want: provider.ExecutionMayHaveOccurred},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := capacityTransport{standard: provider.TransportFunc(func(context.Context, carrier.Document) (provider.Ingress, error) {
				return nil, test.failure
			})}
			meta := carrier.Meta{}
			if test.marked {
				meta.Opaque = map[string]string{serviceTierAutoMarker: "true"}
			}
			_, err := transport.Send(context.Background(), carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(test.document), meta))
			failure, ok := provider.AsAttemptFailure(err)
			if !ok || failure.Execution() != test.want {
				t.Fatalf("failure = %#v, want execution %v", err, test.want)
			}
		})
	}
}

func groqTarget(baseURL string, kind protocolkind.ProtocolKind) provider.TargetSnapshot {
	protocol := "chat_completions"
	if kind == protocolkind.Responses {
		protocol = "responses"
	}
	target := provider.NewTargetSnapshot("groq", string(profile.ProviderSpecGroq), baseURL, "env:GROQ_API_KEY", kind, protocol, delivery.BufferedDelivery())
	target.Model = "future-model"
	return target
}

func groqPayload(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

var _ providersruntime.Discovery = NewRuntime(nil, nil).Discovery
