package openaifamily

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestOpenAIFamilyKernelUsesStandardChatCompletionsTokenField(t *testing.T) {
	maxTokens := 64
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{MaxOutputTokens: &maxTokens})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, Controls: controls,
	})

	for _, tc := range []struct {
		name       string
		providerID profile.ProviderID
		policy     ProviderRoutePolicy
	}{
		{name: "official_openai_kernel", providerID: profile.ProviderSpecOpenAI, policy: NewOpenAIPolicy()},
		{name: "openrouter", providerID: profile.ProviderSpecOpenRouter, policy: NewOpenRouterPolicy()},
		{name: "ollama", providerID: profile.ProviderSpecOllama, policy: NewOllamaPolicy()},
		{name: "custom", providerID: profile.ProviderSpecCustom, policy: NewCustomPolicy()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := provider.NewTargetSnapshot("backend", string(tc.providerID), "https://example.test/v1", "env:TOKEN", protocolkind.ChatCompletions, "", "chat_completions")
			target.Model = request.Model()
			backend, err := NewExecutor(nil, nil, tc.policy).ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload["max_tokens"] != float64(maxTokens) {
				t.Fatalf("max_tokens = %#v, want %d", payload["max_tokens"], maxTokens)
			}
			if _, exists := payload["max_completion_tokens"]; exists {
				t.Fatalf("unexpected provider dialect field in standard kernel: %s", document.RawBytes())
			}
		})
	}
}

func TestOpenAIFamilyTargetsInheritChatCompletionsWebSearch(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, canonical.NewWebSearchDeclaration()),
			canonicaltest.Message(t, canonical.MessageRoleUser, "search"),
		},
	})
	for _, tc := range []struct {
		name       string
		providerID profile.ProviderID
		policy     ProviderRoutePolicy
	}{
		{name: "custom", providerID: profile.ProviderSpecCustom, policy: NewCustomPolicy()},
		{name: "ollama", providerID: profile.ProviderSpecOllama, policy: NewOllamaPolicy()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := provider.NewTargetSnapshot("backend", string(tc.providerID), "https://example.test/v1", "", protocolkind.ChatCompletions, "", "chat_completions")
			target.Model = request.Model()
			backend, err := NewExecutor(nil, nil, tc.policy).ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(document.RawBytes()), `"web_search_options":{}`) {
				t.Fatalf("%s target did not inherit protocol web search: %s", tc.name, document.RawBytes())
			}
		})
	}
}

func TestOpenAIFamilyKernelUsesStandardChatReasoningEffort(t *testing.T) {
	effort := canonical.InferenceEffortHigh
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, Controls: controls,
	})
	for _, tc := range []struct {
		name       string
		providerID profile.ProviderID
		policy     ProviderRoutePolicy
		wantField  string
	}{
		{name: "openrouter_transport_kernel", providerID: profile.ProviderSpecOpenRouter, policy: NewOpenRouterPolicy(), wantField: "reasoning_effort"},
		{name: "openai_standard_effort", providerID: profile.ProviderSpecOpenAI, policy: NewOpenAIPolicy(), wantField: "reasoning_effort"},
		{name: "custom_standard_protocol", providerID: profile.ProviderSpecCustom, policy: NewCustomPolicy(), wantField: "reasoning_effort"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := provider.NewTargetSnapshot("backend", string(tc.providerID), "https://example.test/v1", "env:TOKEN", protocolkind.ChatCompletions, "", "chat_completions")
			target.Model = request.Model()
			backend, err := NewExecutor(nil, nil, tc.policy).ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if _, present := payload[tc.wantField]; !present {
				t.Fatalf("%s payload = %s", tc.name, document.RawBytes())
			}
			if _, present := payload["reasoning"]; present {
				t.Fatalf("%s leaked provider dialect reasoning: %s", tc.name, document.RawBytes())
			}
		})
	}
}

func TestOpenRouterBackendRejectsUnprovenReasoningTokenCeiling(t *testing.T) {
	compute, err := canonical.NewBudgetReasoningCompute(2048)
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(compute)})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:     canonical.Specify("model"),
		Items:     []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		Reasoning: reasoning,
	})
	target := provider.NewTargetSnapshot("backend", string(profile.ProviderSpecOpenRouter), "https://example.test/v1", "env:TOKEN", protocolkind.ChatCompletions, "", "chat_completions")
	target.Model = request.Model()
	backend, err := NewExecutor(nil, nil, NewOpenRouterPolicy()).ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()}); err == nil {
		t.Fatal("OpenRouter backend accepted a hard ceiling without target proof")
	}
}

func TestCustomMessagesReplaysProtocolOpaqueThinking(t *testing.T) {
	opaque, err := canonical.NewMessagesOpaqueThinking([]byte(`{"type":"thinking","thinking":"brief","signature":"client-carried-signature"}`))
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "brief")
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("compatible-model"),
		Items: []canonical.CanonicalItem{reasoning, canonicaltest.Message(t, canonical.MessageRoleUser, "again")},
	})
	target := provider.NewTargetSnapshot("custom-target", string(profile.ProviderSpecCustom), "https://example.test/v1", "", protocolkind.Messages, "", "messages")
	target.Model = request.Model()
	backend, err := NewExecutor(nil, nil, NewCustomPolicy()).ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), "client-carried-signature") {
		t.Fatalf("custom Messages endpoint did not replay protocol state: %s", document.RawBytes())
	}
}

func TestCustomMessagesReplaysOpaqueThinking(t *testing.T) {
	opaque, err := canonical.NewMessagesOpaqueThinking([]byte(`{"type":"thinking","thinking":"brief","signature":"custom-provider-signature"}`))
	if err != nil {
		t.Fatal(err)
	}
	part, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "brief")
	reasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	target := provider.NewTargetSnapshot("custom-target", string(profile.ProviderSpecCustom), "https://example.test/v1", "", protocolkind.Messages, "", "messages")
	target.Model = "compatible-model"
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(target.Model),
		Items: []canonical.CanonicalItem{reasoning, canonicaltest.Message(t, canonical.MessageRoleUser, "again")},
	})
	backend, err := NewExecutor(nil, nil, NewCustomPolicy()).ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"signature":"custom-provider-signature"`) {
		t.Fatalf("custom target could not replay its exact state: %s", document.RawBytes())
	}
}

func TestResponsesEncryptedCaptureIsComposedByStandardResponsesCodec(t *testing.T) {
	tool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup"), "lookup", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("reasoning-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, tool),
			canonicaltest.Message(t, canonical.MessageRoleUser, "hi"),
		},
	})
	for _, tc := range []struct {
		name       string
		providerID profile.ProviderID
		policy     ProviderRoutePolicy
		want       bool
	}{
		{name: "official_openai", providerID: profile.ProviderSpecOpenAI, policy: NewOpenAIPolicy(), want: true},
		{name: "custom", providerID: profile.ProviderSpecCustom, policy: NewCustomPolicy(), want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := provider.NewTargetSnapshot("target", string(tc.providerID), "https://example.test/v1", "", protocolkind.Responses, "", "responses")
			target.Model = request.Model()
			backend, err := NewExecutor(nil, nil, tc.policy).ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Contains(string(document.RawBytes()), `"include":["reasoning.encrypted_content"]`)
			if got != tc.want {
				t.Fatalf("encrypted capture presence = %t, want %t: %s", got, tc.want, document.RawBytes())
			}
		})
	}
}

func TestOutboundRequestDebugRedactsStatelessResponsesInput(t *testing.T) {
	raw := []byte(`{"model":"gpt","input":[{"type":"reasoning","encrypted_content":"ciphertext"},{"type":"program","code":"secret program"}],"stream":true}`)
	redacted := string(redactProviderRequestInput(raw))
	if strings.Contains(redacted, "ciphertext") || strings.Contains(redacted, "secret program") || !strings.Contains(redacted, `"input":"[REDACTED]"`) {
		t.Fatalf("redacted request = %s", redacted)
	}
}

type failingRoundTripper struct {
	err error
}

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error { b.closed = true; return nil }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (t failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

func TestSendProviderRequest_PreservesTransportErrorDetail(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("dial tcp 127.0.0.1:11434: connect: connection refused")
	exec := NewExecutor(&http.Client{Transport: failingRoundTripper{err: transportErr}}, nil, NewOllamaPolicy())
	doc := carrier.NewDocument(
		protocolkind.Responses,
		"application/json",
		nil,
		[]byte(`{"model":"gpt-4o-mini","input":"hello"}`),
		carrier.Meta{},
	)
	target := provider.NewTargetSnapshot(
		"backend-a",
		string(profile.ProviderSpecOllama),
		"http://127.0.0.1:11434/v1",
		"",
		protocolkind.Responses,
		"",
		"",
	)
	target.Model = "gpt-4o-mini"

	_, err := exec.Send(context.Background(), target, doc)
	if err == nil {
		t.Fatal("expected SendProviderRequest to fail")
	}
	var unavailable provider.UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error type = %T, want provider.UnavailableError", err)
	}

	var swErr canonical.Error
	if !errors.As(err, &swErr) {
		t.Fatalf("error type = %T, want canonical.Error", err)
	}
	if swErr.Code != canonical.ErrorCodeBadEndpoint {
		t.Fatalf("error code = %s, want %s", swErr.Code, canonical.ErrorCodeBadEndpoint)
	}
	if got := swErr.Details["request_transport_error"]; got != transportErr.Error() {
		t.Fatalf("transport detail = %q, want %q", got, transportErr.Error())
	}
}

func TestSendProviderRequest_MarksConfirmedUnsupportedResponse(t *testing.T) {
	body := io.NopCloser(strings.NewReader(`{"error":{"message":"tool_choice required is unsupported","param":"tool_choice","code":"unsupported_parameter"}}`))
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body, Request: req}, nil
	})}
	exec := NewExecutor(client, nil, NewOllamaPolicy())
	target := provider.NewTargetSnapshot("backend-a", string(profile.ProviderSpecOllama), "http://127.0.0.1:11434/v1", "", protocolkind.Responses, "", "")
	target.Model = "gpt-4o-mini"
	doc := carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{"model":"gpt-4o-mini","tool_choice":"required"}`), carrier.Meta{})

	_, err := exec.Send(context.Background(), target, doc)
	var unsupported provider.IncompatibleTargetError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %T, want provider.IncompatibleTargetError", err)
	}
}

func TestSendProviderRequest_BoundsNonSSEStreamingEvidenceAndClosesBody(t *testing.T) {
	body := &closeTrackingBody{Reader: strings.NewReader(strings.Repeat("x", (64<<10)+4096))}
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body, Request: req}, nil
	})}
	exec := NewExecutor(client, nil, NewOllamaPolicy())
	target := provider.NewTargetSnapshot("backend-a", string(profile.ProviderSpecOllama), "http://127.0.0.1:11434/v1", "", protocolkind.Responses, "", "")
	target.Model = "gpt-4o-mini"
	doc := carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{"model":"gpt-4o-mini","input":"hello","stream":true}`), carrier.Meta{})
	_, err := exec.Send(context.Background(), target, doc)
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) || backendErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("error = %#v, want bounded 502 backend error", err)
	}
	if len(backendErr.Message) > 64<<10 {
		t.Fatalf("backend evidence length = %d, want <= %d", len(backendErr.Message), 64<<10)
	}
	if !body.closed {
		t.Fatal("non-SSE response body was not closed")
	}
}
