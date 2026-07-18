package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/wire"
)

type testCredentialResolver struct{}

func (testCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "token_test", nil
}

type recordingEffectSink struct {
	effects []effect.Effect
}

func (s *recordingEffectSink) Commit(_ context.Context, _ string, effects []effect.Effect) error {
	s.effects = append(s.effects, effects...)
	return nil
}

func mustJSONBodyMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return body
}

func mustProviderRequestWithDocument(t *testing.T, request canonical.CanonicalRequest, contract exchange.ExecutionContract, target exchange.RoutableTarget) exchange.ProviderRequest {
	t.Helper()
	codec := codecresolver.NewRuntimeCodecResolver().ProviderRequestDocumentEncoder(target.ProtocolKind)
	if codec == nil {
		t.Fatalf("provider request encoder missing for protocol %s", target.ProtocolKind)
	}
	wireRequestResult, err := codec.EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: request}, contract.ProviderDelivery, "")
	if err != nil {
		t.Fatalf("encode provider request document: %v", err)
	}
	return exchange.NewProviderRequest("test-ex", protocolkind.Responses, request, wireRequestResult.Value, contract, target)
}

type compatExpectation struct {
	feature compat.Feature
	outcome compat.Outcome
}

func assertCompatibilityEffects(t *testing.T, sink *recordingEffectSink, want []compatExpectation, subject compat.Subject) {
	t.Helper()
	if len(sink.effects) != len(want) {
		t.Fatalf("captured effects len=%d want=%d", len(sink.effects), len(want))
	}
	for i, effectItem := range sink.effects {
		compatEffect, ok := effectItem.(effect.CompatibilityEffect)
		if !ok {
			t.Fatalf("effect[%d] type = %T, want effect.CompatibilityEffect", i, effectItem)
		}
		if compatEffect.Feature != want[i].feature || compatEffect.Outcome != want[i].outcome {
			t.Fatalf("effect[%d] = %#v, want %s %s", i, compatEffect, want[i].feature, want[i].outcome)
		}
		if compatEffect.Subject != subject {
			t.Fatalf("effect[%d] subject = %q, want %q", i, compatEffect.Subject, subject)
		}
	}
}

func TestServices_ExecutionDispatchesByProviderID(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		case "/v1/messages":
			_, _ = w.Write([]byte(`{"id":"msg_1","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"unexpected path"}`))
		}
	}))
	defer upstream.Close()

	composition := NewProviderRegistry(upstream.Client(), testCredentialResolver{})

	openAIReq := mustProviderRequestWithDocument(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "chat_completions"),
	)
	if _, err := composition.ResolveProviderIngress(context.Background(), openAIReq); err != nil {
		t.Fatalf("openai execution failed: %v", err)
	}

	anthropicReq := mustProviderRequestWithDocument(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-b", "anthropic", upstream.URL+"/v1", "cred-1", protocolkind.Messages, "credential_ref", "", "messages"),
	)
	if _, err := composition.ResolveProviderIngress(context.Background(), anthropicReq); err != nil {
		t.Fatalf("anthropic execution failed: %v", err)
	}
}

func TestServices_ModelCatalogDispatchesByProviderID(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"},{"id":"model-b"}]}`))
	}))
	defer upstream.Close()

	composition := NewProviderRegistry(upstream.Client(), testCredentialResolver{})

	openAIModels, err := composition.ListDeployments(context.Background(), exchange.NewRoutableTarget(
		"backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "",
	))
	if err != nil {
		t.Fatalf("openai model catalog failed: %v", err)
	}
	if len(openAIModels) != 2 {
		t.Fatalf("openai model catalog len=%d want 2", len(openAIModels))
	}

	_, err = composition.ListDeployments(context.Background(), exchange.NewRoutableTarget(
		"backend-b", "chatgpt", upstream.URL+"/v1", "keychain:chatgpt/default", protocolkind.ChatCompletions, "credential_ref", "", "",
	))
	if err == nil || !strings.Contains(err.Error(), "subscription tier") {
		t.Fatalf("chatgpt catalog dispatch must use chatgpt adapter tier validation, got err=%v", err)
	}
}

func TestServices_UnknownProviderIDFailsFast(t *testing.T) {
	t.Parallel()

	composition := NewProviderRegistry(http.DefaultClient, testCredentialResolver{})
	_, err := composition.ResolveProviderIngress(context.Background(), mustProviderRequestWithDocument(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:     "m",
			InputText: "hi",
		}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-a", "unknown-provider", "https://example.test/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", ""),
	))
	if err == nil || !strings.Contains(err.Error(), "provider id is unsupported") {
		t.Fatalf("unknown provider must fail fast, got err=%v", err)
	}
}

func TestServices_ValidateCredentialsDispatchesByProviderID(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"}]}`))
	}))
	defer upstream.Close()

	composition := NewProviderRegistry(upstream.Client(), testCredentialResolver{})
	err := composition.ValidateCredentials(context.Background(), exchange.NewRoutableTarget(
		"backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "",
	))
	if err != nil {
		t.Fatalf("openai validate credentials failed: %v", err)
	}
}

func TestServices_OpenAIFamilyDoesNotEmitCacheCompatibilityDecisions(t *testing.T) {
	t.Parallel()

	var seenBody []byte
	var readErr error
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		seenBody, readErr = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
	})
	composition := NewProviderRegistry(upstream.Client(), testCredentialResolver{})
	sink := &recordingEffectSink{}
	req := mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-openai", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "chat_completions"),
	)
	req.EffectSink = sink
	req.ExchangeID = "ex-cache-intent-openai"

	if _, err := composition.ResolveProviderIngress(context.Background(), req); err != nil {
		t.Fatalf("openai execution failed: %v", err)
	}
	if readErr != nil {
		t.Fatalf("read upstream body: %v", readErr)
	}

	body := mustJSONBodyMap(t, seenBody)
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key must be omitted")
	}
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention must be omitted")
	}
	if len(sink.effects) != 0 {
		t.Fatalf("compatibility effects len=%d want 0", len(sink.effects))
	}
}

func TestServices_OpenAIFamilyDoesNotEmitCacheFieldsOnOllama(t *testing.T) {
	t.Parallel()

	var seenBody []byte
	var readErr error
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		seenBody, readErr = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
	})
	composition := NewProviderRegistry(upstream.Client(), testCredentialResolver{})
	sink := &recordingEffectSink{}
	req := mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-ollama", "ollama", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "chat_completions"),
	)
	req.EffectSink = sink
	req.ExchangeID = "ex-cache-intent-ollama"

	if _, err := composition.ResolveProviderIngress(context.Background(), req); err != nil {
		t.Fatalf("ollama execution failed: %v", err)
	}
	if readErr != nil {
		t.Fatalf("read upstream body: %v", readErr)
	}

	body := mustJSONBodyMap(t, seenBody)
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key must be omitted on ollama")
	}
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention must be omitted on ollama")
	}
	if len(sink.effects) != 0 {
		t.Fatalf("compatibility effects len=%d want 0", len(sink.effects))
	}
}

func TestServices_OpenAIFamilyEmitsStructuredOutputCompatibilityDecisions(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	outputFormat, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:        canonical.OutputFormatJSONSchema,
		Name:        "reply_shape",
		Description: "structured reply",
		Schema:      canonical.NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`),
		Strict:      true,
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}
	strict := true
	tool := canonical.NewFunctionToolDecl("tool_0", "search", "search the workspace", canonical.NewToolSchemaObject(`{"type":"object","properties":{"q":{"type":"string"}}}`))
	tool.Strict = &strict
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "m",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Tools:        []canonical.ToolDecl{tool},
		OutputFormat: outputFormat,
	})
	composition := NewProviderRegistry(upstream.Client(), testCredentialResolver{})
	sink := &recordingEffectSink{}
	req := mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-openai", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "chat_completions"),
	)
	req.EffectSink = sink
	req.ExchangeID = "ex-structured-output"

	if _, err := composition.ResolveProviderIngress(context.Background(), req); err != nil {
		t.Fatalf("openai execution failed: %v", err)
	}

	assertCompatibilityEffects(t, sink, []compatExpectation{
		{feature: compat.RequestStructuredOutput, outcome: compat.Exact},
		{feature: compat.ToolDeclaration, outcome: compat.Exact},
		{feature: compat.OutputFormat, outcome: compat.Exact},
		{feature: compat.OutputJSONSchema, outcome: compat.Exact},
		{feature: compat.WireJSONMode, outcome: compat.Exact},
		{feature: compat.ToolSchemaStrict, outcome: compat.Exact},
	}, compat.Subject("route:provider/openai/protocol/chat_completions"))
}

func TestServices_BedrockEmitsToolSchemaStrictDropCompatibilityDecision(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")

	strict := true
	tool := canonical.NewFunctionToolDecl("tool_0", "search", "search the workspace", canonical.NewToolSchemaObject(`{"type":"object","properties":{"q":{"type":"string"}}}`))
	tool.Strict = &strict
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Tools: []canonical.ToolDecl{tool},
	})
	composition := NewProviderRegistry(upstream.Client(), testCredentialResolver{})
	sink := &recordingEffectSink{}
	req := mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-bedrock", "bedrock", upstream.URL, "env:AWS_BEARER_TOKEN_BEDROCK", protocolkind.Messages, "credential_ref", "", "messages"),
	)
	req.EffectSink = sink
	req.ExchangeID = "ex-bedrock-strict-drop"

	if _, err := composition.ResolveProviderIngress(context.Background(), req); err != nil {
		t.Fatalf("bedrock execution failed: %v", err)
	}

	assertCompatibilityEffects(t, sink, []compatExpectation{
		{feature: compat.ToolDeclaration, outcome: compat.Exact},
		{feature: compat.ToolSchemaStrict, outcome: compat.Drop},
	}, compat.Subject("route:provider/bedrock/protocol/messages"))
}

func TestServices_AnthropicEmitsToolSchemaStrictDropCompatibilityDecision(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	strict := true
	tool := canonical.NewFunctionToolDecl("tool_0", "search", "search the workspace", canonical.NewToolSchemaObject(`{"type":"object","properties":{"q":{"type":"string"}}}`))
	tool.Strict = &strict
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		Tools: []canonical.ToolDecl{tool},
	})
	composition := NewProviderRegistry(upstream.Client(), testCredentialResolver{})
	sink := &recordingEffectSink{}
	req := mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-anthropic", "anthropic", upstream.URL+"/v1", "credential-ref", protocolkind.Messages, "credential_ref", "", "messages"),
	)
	req.EffectSink = sink
	req.ExchangeID = "ex-anthropic-strict-drop"

	if _, err := composition.ResolveProviderIngress(context.Background(), req); err != nil {
		t.Fatalf("anthropic execution failed: %v", err)
	}

	assertCompatibilityEffects(t, sink, []compatExpectation{
		{feature: compat.ToolDeclaration, outcome: compat.Exact},
		{feature: compat.ToolSchemaStrict, outcome: compat.Drop},
	}, compat.Subject("route:provider/anthropic/protocol/messages"))
}

func TestServices_OpenAIFamilyEmitsBackendErrorClassCompatibilityDecision(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"tool_choice required is unsupported","param":"tool_choice","code":"unsupported_parameter"}}`))
	}))
	defer upstream.Close()

	composition := NewProviderRegistry(upstream.Client(), testCredentialResolver{})
	sink := &recordingEffectSink{}
	req := mustProviderRequestWithDocument(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-openai", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "credential_ref", "", "chat_completions"),
	)
	req.EffectSink = sink
	req.ExchangeID = "ex-error-class"

	_, err := composition.ResolveProviderIngress(context.Background(), req)
	if err == nil {
		t.Fatal("expected backend error")
	}
	if !canonical.IsBackendErrorClass(err, canonical.BackendErrorClassToolChoiceUnsupported) {
		t.Fatalf("expected classified backend error, got %v", err)
	}
	if len(sink.effects) != 1 {
		t.Fatalf("captured effects len=%d want=1", len(sink.effects))
	}
	compatEffect, ok := sink.effects[0].(effect.CompatibilityEffect)
	if !ok {
		t.Fatalf("captured effect type = %T, want effect.CompatibilityEffect", sink.effects[0])
	}
	if compatEffect.Feature != compat.ErrorClass || compatEffect.Outcome != compat.Approx {
		t.Fatalf("captured effect = %#v, want error.class approx", compatEffect)
	}
	if compatEffect.Subject != compat.Subject("route:provider/openai/protocol/chat_completions/error_class/tool_choice_unsupported") {
		t.Fatalf("captured subject = %q, want class-specific route subject", compatEffect.Subject)
	}
}

func TestServices_RejectsUnsupportedStructuredOutputBeforeEncoding(t *testing.T) {
	t.Parallel()

	composition := NewProviderRegistry(http.DefaultClient, testCredentialResolver{})
	outputFormat, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:        canonical.OutputFormatJSONSchema,
		Name:        "reply_shape",
		Schema:      canonical.NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`),
		Strict:      true,
		Description: "structured reply",
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "m",
		Items:        []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		OutputFormat: outputFormat,
	})
	req := exchange.NewProviderRequest(
		"test-ex", protocolkind.Responses, request,
		carrier.CarrierDocument{},
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-b", "anthropic", "https://example.test/v1", "cred-1", protocolkind.Messages, "credential_ref", "", "messages"),
	)
	req.ExchangeID = "ex-structured-output"
	sink := &recordingEffectSink{}
	req.EffectSink = sink

	_, err = composition.ResolveProviderIngress(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "structured JSON schema output") {
		t.Fatalf("structured output should fail closed before encoding, got err=%v", err)
	}
	if len(sink.effects) != 1 {
		t.Fatalf("captured effect len=%d want=1", len(sink.effects))
	}
	compatEffect, ok := sink.effects[0].(effect.CompatibilityEffect)
	if !ok {
		t.Fatalf("captured effect type = %T, want effect.CompatibilityEffect", sink.effects[0])
	}
	if compatEffect.Feature != compat.RequestStructuredOutput || compatEffect.Outcome != compat.Reject {
		t.Fatalf("captured effect = %#v, want request.structured_output reject", compatEffect)
	}
}
