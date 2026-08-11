package openaifamily

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

func newPolicyTarget(providerID profile.ProviderID, baseURL, credential string, kind protocolkind.ProtocolKind, providerProtocol string) provider.TargetSnapshot {
	if providerID == profile.ProviderSpecCustom {
		return provider.NewCustomTargetSnapshot("backend", baseURL, credential, kind, "", providerProtocol, "Authorization")
	}
	return provider.NewTargetSnapshot("backend", string(providerID), baseURL, credential, kind, "", providerProtocol)
}

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
		{name: "lmstudio", providerID: profile.ProviderSpecLMStudio, policy: NewLMStudioPolicy()},
		{name: "vllm", providerID: profile.ProviderSpecVLLM, policy: NewVLLMPolicy()},
		{name: "custom", providerID: profile.ProviderSpecCustom, policy: NewCustomPolicy()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := newPolicyTarget(tc.providerID, "https://example.test/v1", "env:TOKEN", protocolkind.ChatCompletions, "chat_completions")
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
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		providerID profile.ProviderID
		policy     ProviderRoutePolicy
	}{
		{name: "custom", providerID: profile.ProviderSpecCustom, policy: NewCustomPolicy()},
		{name: "ollama", providerID: profile.ProviderSpecOllama, policy: NewOllamaPolicy()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := newPolicyTarget(tc.providerID, "https://example.test/v1", "", protocolkind.ChatCompletions, "chat_completions")
			target.Model = request.Model()
			backend, err := NewExecutor(nil, nil, tc.policy).ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(document.RawBytes()), `"web_search_options":{}`) {
				t.Fatalf("%s target did not inherit protocol web search: %s", tc.name, document.RawBytes())
			}
		})
	}
}

func TestCommodityResponsesTargetsUseFlatNamespaceGrammar(t *testing.T) {
	childKey, _ := canonical.NewToolKey("workspace", canonical.ToolKindFunction, "read_file")
	child := canonicaltest.MustFunctionTool(childKey, "Read", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	namespaceKey, _ := canonical.NewRequestToolKey(canonical.ToolKindNamespace, "workspace")
	namespace, _ := canonical.NewToolNamespace(namespaceKey, "Workspace", []canonical.ToolDeclaration{child})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, namespace), canonicaltest.Message(t, canonical.MessageRoleUser, "read")},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		provider profile.ProviderID
		policy   ProviderRoutePolicy
		baseURL  string
	}{
		{name: "ollama", provider: profile.ProviderSpecOllama, policy: NewOllamaPolicy(), baseURL: "http://127.0.0.1:11434/v1"},
		{name: "lmstudio", provider: profile.ProviderSpecLMStudio, policy: NewLMStudioPolicy(), baseURL: "http://127.0.0.1:1234/v1"},
		{name: "vllm", provider: profile.ProviderSpecVLLM, policy: NewVLLMPolicy(), baseURL: "http://127.0.0.1:8000/v1"},
		{name: "custom", provider: profile.ProviderSpecCustom, policy: NewCustomPolicy(), baseURL: "http://127.0.0.1:8080/v1"},
		{name: "lmstudio_custom", provider: profile.ProviderSpecCustom, policy: NewCustomPolicy(), baseURL: "http://127.0.0.1:1234/v1"},
		{name: "llama_cpp_custom", provider: profile.ProviderSpecCustom, policy: NewCustomPolicy(), baseURL: "http://127.0.0.1:8081/v1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := newPolicyTarget(test.provider, test.baseURL, "", protocolkind.Responses, "responses")
			target.TargetID = test.name
			target.Model = "model"
			backend, err := NewExecutor(nil, nil, test.policy).ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			raw := string(document.RawBytes())
			wireName, _ := names.WireName(childKey)
			if strings.Contains(raw, `"type":"namespace"`) || !strings.Contains(raw, `"name":"`+wireName+`"`) {
				t.Fatalf("flat Responses document = %s", raw)
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
			target := newPolicyTarget(tc.providerID, "https://example.test/v1", "env:TOKEN", protocolkind.ChatCompletions, "chat_completions")
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

func TestClineReasoningExcludeIsOmittedFromCustomOpenAICompatibleProviderRequest(t *testing.T) {
	clientDocument := carrier.NewDocument(
		protocolkind.ChatCompletions,
		"application/json",
		nil,
		[]byte(`{"model":"zai-org/GLM-5.1","messages":[{"role":"user","content":"Hello"}],"temperature":0.25,"reasoning":{"exclude":true}}`),
		carrier.Meta{},
	)
	decoded, err := (chatcompletions.ClientRequestDecoder{}).DecodeClientRequest(clientDocument)
	if err != nil {
		t.Fatal(err)
	}
	request := decoded.Request.Request
	target := newPolicyTarget(profile.ProviderSpecCustom, "https://api.friendli.ai/serverless/v1", "env:FRIENDLI_TOKEN", protocolkind.ChatCompletions, "chat_completions")
	target.Model = request.Model()
	backend, err := NewExecutor(nil, nil, NewCustomPolicy()).ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: decoded.Request.Delivery})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, present := payload["reasoning"]; present {
		t.Fatalf("custom OpenAI-compatible provider request retained Cline reasoning dialect: %s", document.RawBytes())
	}
	if payload["temperature"] != 0.25 || payload["model"] != "zai-org/GLM-5.1" {
		t.Fatalf("ordinary Chat fields did not survive normalization: %s", document.RawBytes())
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages did not survive normalization: %s", document.RawBytes())
	}
}

func TestOpenRouterTransportKernelProjectsStandardReasoningBudget(t *testing.T) {
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
	document, changes, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["reasoning_effort"] != "low" {
		t.Fatalf("reasoning_effort = %#v, want low", payload["reasoning_effort"])
	}
	if len(changes) != 1 ||
		changes[0].Capability != canonical.RequestReasoning ||
		changes[0].Preserved != canonical.RequestControlsEffort {
		t.Fatalf("changes = %#v, want one reasoning-to-effort approximation", changes)
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
	target := provider.NewCustomTargetSnapshot("custom-target", "https://example.test/v1", "", protocolkind.Messages, "", "messages", "")
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
	target := provider.NewCustomTargetSnapshot("custom-target", "https://example.test/v1", "", protocolkind.Messages, "", "messages", "")
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
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
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
			target := newPolicyTarget(tc.providerID, "https://example.test/v1", "", protocolkind.Responses, "responses")
			target.Model = request.Model()
			backend, err := NewExecutor(nil, nil, tc.policy).ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
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

func TestOutboundRequestDebugLogsStructureWithoutContent(t *testing.T) {
	const canary = "SWOBU_PRIVATE_REQUEST_CANARY"
	requests := [][]byte{
		[]byte(`{"model":"gpt","messages":[{"role":"user","content":"` + canary + `"}],"tools":[{"function":{"description":"` + canary + `"}}]}`),
		[]byte(`{"model":"claude","system":"` + canary + `","messages":[{"role":"user","content":"` + canary + `"}]}`),
		[]byte(`{"model":"gpt","instructions":"` + canary + `","input":[{"type":"reasoning","encrypted_content":"` + canary + `"}]}`),
		[]byte(`{"malformed":"` + canary),
	}

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	for _, request := range requests {
		logOpenAIFamilyOutboundRequest("openai", "responses", "/v1/responses", request)
	}

	got := logs.String()
	if strings.Contains(got, canary) {
		t.Fatalf("outbound request content reached logs: %s", got)
	}
	for _, structural := range []string{"provider_spec=openai", "provider_protocol=responses", "path=/v1/responses", "body_bytes="} {
		if !strings.Contains(got, structural) {
			t.Fatalf("logs missing structural field %q: %s", structural, got)
		}
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
	failure, ok := provider.AsAttemptFailure(err)
	if !ok || failure.Execution() != provider.ExecutionMayHaveOccurred {
		t.Fatalf("attempt failure = %#v, %t", failure, ok)
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

func TestSendProviderRequest_PreservesTransportCancellation(t *testing.T) {
	exec := NewExecutor(
		&http.Client{Transport: failingRoundTripper{err: context.Canceled}},
		nil,
		NewOllamaPolicy(),
	)
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
	var cancelled provider.CancelledError
	if !errors.As(err, &cancelled) {
		t.Fatalf("error type = %T, want provider.CancelledError", err)
	}
	var swErr canonical.Error
	if !errors.As(err, &swErr) || swErr.Code != canonical.ErrorCodeBadEndpoint {
		t.Fatalf("diagnostic error = %#v, want BAD_ENDPOINT", err)
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
	failure, ok := provider.AsAttemptFailure(err)
	if !ok || failure.Execution() != provider.ExecutionRejectedBeforeExecution {
		t.Fatalf("attempt failure = %#v, %t", failure, ok)
	}
}

// An unclassified 4xx defaults to RejectedError with ExecutionMayHaveOccurred.
// This owns the adapter's fallback for unclassified rejection, not permanent
// ignorance of any provider's vocabulary: the prose is deliberately opaque so
// that adding narrower recognition later can only narrow, never break, this
// path. The route reducer combines this fact with replay safety, not here.
func TestSendProviderRequest_Unclassified4xxDefaultsToRejectedAndMayHaveExecuted(t *testing.T) {
	body := io.NopCloser(strings.NewReader(`{"error":{"message":"opaque-unclassified-rejection-7f3a"}}`))
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body, Request: req}, nil
	})}
	exec := NewExecutor(client, nil, NewOllamaPolicy())
	target := provider.NewTargetSnapshot("backend-a", string(profile.ProviderSpecOllama), "http://127.0.0.1:11434/v1", "", protocolkind.Responses, "", "")
	target.Model = "gpt-4o-mini"
	doc := carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{"model":"gpt-4o-mini"}`), carrier.Meta{})

	_, err := exec.Send(context.Background(), target, doc)
	var rejected provider.RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("error = %T, want provider.RejectedError", err)
	}
	failure, ok := provider.AsAttemptFailure(err)
	if !ok || failure.Execution() != provider.ExecutionMayHaveOccurred {
		t.Fatalf("attempt failure = %#v, %t, want ExecutionMayHaveOccurred", failure, ok)
	}
}

func TestSendProviderRequest_MarksPreDispatchValidation(t *testing.T) {
	exec := NewExecutor(nil, nil, NewOllamaPolicy())
	target := provider.NewTargetSnapshot(
		"backend-a",
		string(profile.ProviderSpecOllama),
		"",
		"",
		protocolkind.Responses,
		"",
		"",
	)
	target.Model = "gpt-4o-mini"
	doc := carrier.NewDocument(
		protocolkind.Responses,
		"application/json",
		nil,
		[]byte(`{"model":"gpt-4o-mini","input":"hello"}`),
		carrier.Meta{},
	)

	_, err := exec.Send(context.Background(), target, doc)
	failure, ok := provider.AsAttemptFailure(err)
	if !ok || failure.Execution() != provider.ExecutionNotDispatched {
		t.Fatalf("attempt failure = %#v, %t", failure, ok)
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
