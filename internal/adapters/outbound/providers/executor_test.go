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
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type testCredentialResolver struct{}

func (testCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "token_test", nil
}

type recordingChanges = []compat.Change

func mustJSONBodyMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return body
}

type testProviderRequest struct {
	Request    canonical.CanonicalRequest
	Contract   exchange.ExecutionContract
	Target     provider.TargetSnapshot
	ExchangeID string
	Changes    *[]compat.Change
}

func newTestProviderRequest(exchangeID string, _ any, request canonical.CanonicalRequest, _ carrier.Document, contract exchange.ExecutionContract, target provider.TargetSnapshot, sinks ...*[]compat.Change) testProviderRequest {
	var sink *[]compat.Change
	if len(sinks) > 0 {
		sink = sinks[0]
	}
	return testProviderRequest{Request: request, Contract: contract, Target: target, ExchangeID: exchangeID, Changes: sink}
}

func mustProviderRequestWithDocument(t *testing.T, request canonical.CanonicalRequest, contract exchange.ExecutionContract, target provider.TargetSnapshot) testProviderRequest {
	t.Helper()
	return newTestProviderRequest("test-ex", protocolkind.Responses, request, carrier.Document{}, contract, target)
}

func executeProviderRequest(registry ProviderRegistry, ctx context.Context, req testProviderRequest) (provider.Ingress, error) {
	target := req.Target.Clone()
	target.Model = req.Request.Model()
	backend, err := registry.ResolveBackend(target)
	if err != nil {
		return nil, err
	}
	names, _, err := provider.BuildAttemptToolNames(req.Request)
	if err != nil {
		return nil, err
	}
	doc, changes, err := backend.Codec.Encode(provider.Request{Canonical: req.Request, ToolNames: names, Delivery: req.Contract.ProviderDelivery})
	if req.Changes != nil && len(changes) > 0 {
		*req.Changes = append(*req.Changes, compat.CloneChanges(changes)...)
	}
	if err != nil {
		return nil, err
	}
	return backend.Transport.Send(ctx, doc)
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

	composition := mustProviderRegistry(t, upstream.Client(), testCredentialResolver{})

	openAIReq := mustProviderRequestWithDocument(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("m"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		provider.NewTargetSnapshot("backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "", "chat_completions"),
	)
	if _, err := executeProviderRequest(composition, context.Background(), openAIReq); err != nil {
		t.Fatalf("openai execution failed: %v", err)
	}

	anthropicReq := mustProviderRequestWithDocument(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("m"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		provider.NewTargetSnapshot("backend-b", "anthropic", upstream.URL+"/v1", "cred-1", protocolkind.Messages, "", "messages"),
	)
	if _, err := executeProviderRequest(composition, context.Background(), anthropicReq); err != nil {
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

	composition := mustProviderRegistry(t, upstream.Client(), testCredentialResolver{})

	openAIProbe, err := composition.ProbeTarget(context.Background(), provider.NewTargetSnapshot(
		"backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "", ""))
	if err != nil {
		t.Fatalf("openai model catalog failed: %v", err)
	}
	if len(openAIProbe.Deployments) != 2 {
		t.Fatalf("openai model catalog len=%d want 2", len(openAIProbe.Deployments))
	}

	_, err = composition.ProbeTarget(context.Background(), provider.NewTargetSnapshot(
		"backend-b", "chatgpt", upstream.URL+"/v1", "secret:chatgpt/default", protocolkind.ChatCompletions, "", ""))
	if err == nil || !strings.Contains(err.Error(), "subscription tier") {
		t.Fatalf("chatgpt catalog dispatch must use chatgpt adapter tier validation, got err=%v", err)
	}
}

func TestServices_UnknownProviderIDFailsFast(t *testing.T) {
	t.Parallel()

	composition := mustProviderRegistry(t, http.DefaultClient, testCredentialResolver{})
	_, err := executeProviderRequest(composition, context.Background(), mustProviderRequestWithDocument(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("m"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		provider.NewTargetSnapshot("backend-a", "unknown-provider", "https://example.test/v1", "cred-1", protocolkind.ChatCompletions, "", ""),
	))
	if err == nil || !strings.Contains(err.Error(), "provider id is unsupported") {
		t.Fatalf("unknown provider must fail fast, got err=%v", err)
	}
}

func TestServices_ProbeTargetDispatchesByProviderID(t *testing.T) {
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

	composition := mustProviderRegistry(t, upstream.Client(), testCredentialResolver{})
	_, err := composition.ProbeTarget(context.Background(), provider.NewTargetSnapshot(
		"backend-a", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "", ""))
	if err != nil {
		t.Fatalf("openai target probe failed: %v", err)
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
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})
	composition := mustProviderRegistry(t, upstream.Client(), testCredentialResolver{})
	sink := &recordingChanges{}
	req := mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		provider.NewTargetSnapshot("backend-openai", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "", "chat_completions"),
	)
	req.Changes = sink
	req.ExchangeID = "ex-cache-intent-openai"

	if _, err := executeProviderRequest(composition, context.Background(), req); err != nil {
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
	if len(*sink) != 0 {
		t.Fatalf("compatibility changes len=%d want 0", len(*sink))
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
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})
	composition := mustProviderRegistry(t, upstream.Client(), testCredentialResolver{})
	sink := &recordingChanges{}
	req := mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		provider.NewTargetSnapshot("backend-ollama", "ollama", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "", "chat_completions"),
	)
	req.Changes = sink
	req.ExchangeID = "ex-cache-intent-ollama"

	if _, err := executeProviderRequest(composition, context.Background(), req); err != nil {
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
	if len(*sink) != 0 {
		t.Fatalf("compatibility changes len=%d want 0", len(*sink))
	}
}

func TestServices_OpenAIProviderReportsActualCompatibilityDecisions(t *testing.T) {
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
	tool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "tool_0"), "search the workspace", canonicaltest.Schema(t, `{"type":"object","properties":{"q":{"type":"string"}}}`), canonical.Specify(true))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, tool),
			canonicaltest.Message(t, canonical.MessageRoleUser, "hi"),
		},
		OutputFormat: canonical.Specify(outputFormat),
	})
	composition := mustProviderRegistry(t, upstream.Client(), testCredentialResolver{})
	sink := &recordingChanges{}
	req := mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		provider.NewTargetSnapshot("backend-openai", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "", "chat_completions"),
	)
	req.Changes = sink
	req.ExchangeID = "ex-structured-output"

	if _, err := executeProviderRequest(composition, context.Background(), req); err != nil {
		t.Fatalf("openai execution failed: %v", err)
	}

	if len(*sink) != 0 {
		t.Fatalf("exact structured-output lowering changes = %#v", *sink)
	}
}

func TestServices_BedrockCodecReportsActualProviderCompatibilityDecisions(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/v1/messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")

	tool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "tool_0"), "search the workspace", canonicaltest.Schema(t, `{"type":"object","properties":{"q":{"type":"string"}}}`), canonical.Specify(true))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, tool),
			canonicaltest.Message(t, canonical.MessageRoleUser, "hi"),
		},
	})
	composition := mustProviderRegistry(t, upstream.Client(), testCredentialResolver{})
	sink := &recordingChanges{}
	req := mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		provider.NewBedrockTargetSnapshot("backend-bedrock", upstream.URL+"/anthropic/v1", "env:AWS_BEARER_TOKEN_BEDROCK", protocolkind.Messages, "", "messages", "us-east-1"),
	)
	req.Changes = sink
	req.ExchangeID = "ex-bedrock-strict-drop"

	if _, err := executeProviderRequest(composition, context.Background(), req); err != nil {
		t.Fatalf("bedrock execution failed: %v", err)
	}

	assertProviderDecision(t, *sink, canonical.RequestToolsSchemaStrict, compat.Omission)
}

func TestServices_AnthropicCodecReportsActualProviderCompatibilityDecisions(t *testing.T) {
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

	tool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "tool_0"), "search the workspace", canonicaltest.Schema(t, `{"type":"object","properties":{"q":{"type":"string"}}}`), canonical.Specify(true))
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, tool),
			canonicaltest.Message(t, canonical.MessageRoleUser, "hi"),
		},
	})
	composition := mustProviderRegistry(t, upstream.Client(), testCredentialResolver{})
	sink := &recordingChanges{}
	req := mustProviderRequestWithDocument(t,
		request,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		provider.NewTargetSnapshot("backend-anthropic", "anthropic", upstream.URL+"/v1", "credential-ref", protocolkind.Messages, "", "messages"),
	)
	req.Changes = sink
	req.ExchangeID = "ex-anthropic-strict-drop"

	if _, err := executeProviderRequest(composition, context.Background(), req); err != nil {
		t.Fatalf("anthropic execution failed: %v", err)
	}

	assertProviderDecision(t, *sink, canonical.RequestToolsSchemaStrict, compat.Omission)
}

func TestServices_OpenAIFamilyClassifiesBackendErrorWithoutTelemetryAuthority(t *testing.T) {
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

	composition := mustProviderRegistry(t, upstream.Client(), testCredentialResolver{})
	req := mustProviderRequestWithDocument(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("m"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		}),
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		provider.NewTargetSnapshot("backend-openai", "openai", upstream.URL+"/v1", "cred-1", protocolkind.ChatCompletions, "", "chat_completions"),
	)
	_, err := executeProviderRequest(composition, context.Background(), req)
	if err == nil {
		t.Fatal("expected backend error")
	}
	if !canonical.IsBackendErrorClass(err, canonical.BackendErrorClassToolChoiceUnsupported) {
		t.Fatalf("expected classified backend error, got %v", err)
	}
}

func TestServices_MessagesCodecProjectsNativeStructuredOutput(t *testing.T) {
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
		Model:        canonical.Specify("m"),
		Items:        []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		OutputFormat: canonical.Specify(outputFormat),
	})
	for _, providerSpec := range []string{"anthropic", "custom"} {
		t.Run(providerSpec, func(t *testing.T) {
			var providerBody []byte
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				providerBody, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"{}"}],"stop_reason":"end_turn"}`))
			}))
			defer upstream.Close()
			composition := mustProviderRegistry(t, upstream.Client(), testCredentialResolver{})
			var target provider.TargetSnapshot
			if providerSpec == "custom" {
				target = provider.NewCustomTargetSnapshot("backend-b", upstream.URL, "cred-1", protocolkind.Messages, "", "messages", "Authorization")
			} else {
				target = provider.NewTargetSnapshot("backend-b", providerSpec, upstream.URL, "cred-1", protocolkind.Messages, "", "messages")
			}
			req := newTestProviderRequest(
				"test-ex", protocolkind.Responses, request,
				carrier.Document{},
				exchange.NewExecutionContract(delivery.BufferedDelivery()),
				target,
			)
			req.ExchangeID = "ex-structured-output"
			sink := &recordingChanges{}
			req.Changes = sink

			if _, err = executeProviderRequest(composition, context.Background(), req); err != nil {
				t.Fatal(err)
			}
			body := mustJSONBodyMap(t, providerBody)
			outputConfig, ok := body["output_config"].(map[string]any)
			if !ok {
				t.Fatalf("output_config = %#v", body["output_config"])
			}
			format, ok := outputConfig["format"].(map[string]any)
			if !ok || format["type"] != "json_schema" {
				t.Fatalf("output_config.format = %#v", outputConfig["format"])
			}
			assertProviderDecision(t, *sink, canonical.RequestOutputFormat, compat.Approximation)
		})
	}
}

func assertProviderDecision(t *testing.T, changes []compat.Change, feature canonical.CapabilityPath, outcome compat.Kind) {
	t.Helper()
	for _, decision := range changes {
		if decision.Capability == feature && decision.Kind == outcome {
			return
		}
	}
	t.Fatalf("provider changes = %#v, want %s/%v", changes, feature, outcome)
}
