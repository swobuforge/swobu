package exchange

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/turnstate"
)

// TestExchangeMachine_RetryFallback_SelectsSecondBackendOnFirstFailure proves
// the machine retries the next target in the plan when the first returns a
// retryable backend error.
func TestExchangeMachine_RetryFallback_SelectsSecondBackendOnFirstFailure(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls []string
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		if len(calls) == 1 {
			return nil, canonical.NewBackendError(req.Target.BackendRef, http.StatusServiceUnavailable, "down", "")
		}
		return carrier.NewCarrierDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"resp_2","model":"m","output_text":"fallback-ok"}`),
			carrier.Meta{},
		), nil
	})

	sink := &recordingEvidenceSink{}
	ingress := RequestIngress{trafficEvidence: sink, runner: runner}

	out, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "fallback-1",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"messages":[{"role":"user","content":"hi"}]}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Response.Transport.Status != http.StatusOK {
		t.Fatalf("response status = %d, want 200", out.Response.Transport.Status)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d (%v)", len(calls), calls)
	}
	if calls[0] != "backend-a" {
		t.Fatalf("first call should be backend-a, got %s", calls[0])
	}
	if calls[1] != "backend-b" {
		t.Fatalf("second call should be backend-b, got %s", calls[1])
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 terminal traffic event, got %d", len(sink.events))
	}
	if got := sink.events[0].Route().ProviderConfigRef(); got != "backend-b" {
		t.Fatalf("terminal event should report backend-b, got %s", got)
	}
	if got := sink.events[0].Result(); got != trafficevidence.ResultClassSuccess {
		t.Fatalf("terminal event result = %q, want success", got)
	}
	if got := sink.events[0].AttemptCount(); got != 2 {
		t.Fatalf("attempt count = %d, want 2", got)
	}
}

// TestExchangeMachine_NonRetryableFailure_TerminatesImmediately proves that a
// non-retryable failure (400 Bad Request) terminates after the first backend
// with no retry attempt.
func TestExchangeMachine_NonRetryableFailure_TerminatesImmediately(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls []string
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		return nil, canonical.NewBackendError(req.Target.BackendRef, http.StatusBadRequest, "bad request", "")
	})

	sink := &recordingEvidenceSink{}
	ingress := RequestIngress{trafficEvidence: sink, runner: runner}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "non-retryable-1",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"messages":[{"role":"user","content":"hi"}]}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err := err; err == nil {
		t.Fatal("expected error for non-retryable failure, got nil")
	}

	// Should only call one backend, then terminate immediately.
	if len(calls) != 1 {
		t.Fatalf("expected 1 provider call, got %d (%v)", len(calls), calls)
	}
	first := calls[0]

	// Evidence should record the single failed attempt.
	if len(sink.events) != 1 {
		t.Fatalf("expected 1 terminal traffic event, got %d", len(sink.events))
	}
	if got := sink.events[0].Route().ProviderConfigRef(); got != first {
		t.Fatalf("terminal event should report %s, got %s", first, got)
	}
	if got := sink.events[0].Result(); got != trafficevidence.ResultClassBackendError {
		t.Fatalf("terminal event result = %q, want backend_error", got)
	}
	if got := sink.events[0].AttemptCount(); got != 1 {
		t.Fatalf("attempt count = %d, want 1", got)
	}
}

// TestExchangeMachine_RetryFallback_RetriesBothThenExhausts proves that when
// every backend errors, the machine tries each once and returns the last error.
func TestExchangeMachine_RetryFallback_RetriesBothThenExhausts(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls []string
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		return nil, canonical.NewBackendError(req.Target.BackendRef, http.StatusServiceUnavailable, "down", "")
	})

	sink := &recordingEvidenceSink{}
	ingress := RequestIngress{trafficEvidence: sink, runner: runner}
	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "fallback-exhaust",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"messages":[{"role":"user","content":"hi"}]}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err == nil {
		t.Fatal("expected error when plan exhausted, got nil")
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d (%v)", len(calls), calls)
	}
	// The last backend error is propagated as the final error.
	if !strings.Contains(err.Error(), "backend-b") {
		t.Fatalf("expected last backend error, got: %v", err)
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 terminal traffic event, got %d", len(sink.events))
	}
	if got := sink.events[0].AttemptCount(); got != 2 {
		t.Fatalf("attempt count = %d, want 2", got)
	}
}

// TestExchangeMachine_PlanRoute_ProducesDeterministicOrder checks that plan
// building is deterministic across repeated calls with the same parameters.
func TestExchangeMachine_PlanRoute_ProducesDeterministicOrder(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	wr := endpointToWorkspaceRouting(endpoint)
	trace := &routing.Trace{ExchangeID: "test", Workspace: wr.WorkspaceSlug}
	plan := routing.BuildPlan("test", wr.WorkspaceSlug, "m", routeTargets(wr), trace)

	if len(plan) != 2 {
		t.Fatalf("expected plan length 2, got %d", len(plan))
	}
	// Determinism check: same exchangeID produces same order.
	wr2 := endpointToWorkspaceRouting(endpoint)
	plan2 := routing.BuildPlan("test", wr2.WorkspaceSlug, "m", routeTargets(wr2), trace)
	if len(plan2) != 2 {
		t.Fatalf("expected plan2 length 2, got %d", len(plan2))
	}
	if plan[0].Target.ID != plan2[0].Target.ID || plan[1].Target.ID != plan2[1].Target.ID {
		t.Fatalf("plan not deterministic: first=%v second=%v", plan, plan2)
	}
}

func TestBuildPathRecord_PreservesCanonicalRequestSemanticBands(t *testing.T) {
	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openai")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(ref, spec, "https://api.openai.com/v1", "cred-1")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithModelID("provider-model")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	maxTokens := 64
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: &maxTokens,
		StopSequences:   []string{"DONE"},
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}
	outputFormat, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:        canonical.OutputFormatJSONSchema,
		Name:        "route_shape",
		Description: "route-level schema",
		Schema:      canonical.NewRawJSONObject(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`),
		Strict:      true,
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}
	turn := canonical.NewTurnRef("resp_prev")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        "client-model",
		Instructions: "Use native tools for filesystem work.",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
		Tools: []canonical.ToolDecl{
			canonical.NewFunctionToolDecl("tool_1", "search", "search workspace", canonical.NewToolSchemaObject(`{"type":"object","properties":{"q":{"type":"string"}}}`)),
		},
		Turn:          turn,
		ToolPolicy:    canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
		ToolCallBatch: canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne),
		Controls:      controls,
		OutputFormat:  outputFormat,
	})

	endpointName, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	record, err := buildPathRecord(context.Background(), "ex-route-bands", endpointName, providerConfig, delivery.StreamingDelivery(delivery.FramingSSE), request)
	if err != nil {
		t.Fatalf("buildPathRecord returned error: %v", err)
	}

	got := record.Request
	if got.Model() != "provider-model" {
		t.Fatalf("route request model = %q, want provider-model", got.Model())
	}
	if got.Instructions() != "Use native tools for filesystem work." {
		t.Fatalf("route request instructions = %q, want preserved instructions", got.Instructions())
	}
	if len(got.Items()) != 1 || got.Items()[0].Text != "hi" {
		t.Fatalf("route request items = %#v, want original item", got.Items())
	}
	if len(got.Tools()) != 1 {
		t.Fatalf("route request tools len = %d, want 1", len(got.Tools()))
	}
	if got.Turn().IsZero() {
		t.Fatal("route request lost turn reference")
	}
	if got.ToolPolicy().Mode != canonical.ToolPolicyRequired {
		t.Fatalf("route request tool policy = %q, want required", got.ToolPolicy().Mode)
	}
	if got.ToolCallBatch().Mode != canonical.ToolCallBatchAtMostOne {
		t.Fatalf("route request tool batch = %q, want at_most_one", got.ToolCallBatch().Mode)
	}
	if gotMax, ok := got.Controls().Limits.MaxOutputTokens.Value(); !ok || gotMax != 64 {
		t.Fatalf("route request max output tokens = (%d, %v), want (64, true)", gotMax, ok)
	}
	if stops := got.Controls().Limits.StopSequences; len(stops) != 1 || stops[0] != "DONE" {
		t.Fatalf("route request stop sequences = %#v, want [DONE]", stops)
	}
	if gotFormat := got.OutputFormat(); gotFormat.Kind != canonical.OutputFormatJSONSchema || gotFormat.Name != "route_shape" || !gotFormat.Strict {
		t.Fatalf("route request output format = %#v, want route schema", gotFormat)
	}
}

func TestResponsesRouteToProviderEncode_PreservesToolSurface(t *testing.T) {
	resolver := codecresolver.NewRuntimeCodecResolver()
	clientCodec := resolver.ClientCodec(canonical.ClientFamilyResponses)
	clientRequest := []byte(`{
		"model":"client-model",
		"input":"edit a file",
		"parallel_tool_calls":false,
		"tools":[{
			"type":"function",
			"name":"exec_command",
			"description":"run a command",
			"parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}
		}]
	}`)
	decoded, err := clientCodec.DecodeClientRequest(carrier.NewCarrierDocument(
		carrier.StageClientRequestIn,
		protocolkind.Responses,
		"application/json",
		nil,
		clientRequest,
		carrier.Meta{},
	))
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}

	ref, err := endpointintent.ParseProviderConfigRef("primary")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("azure")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(ref, spec, "contact-8837-resource", "keychain")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithProviderProtocol("responses_stream")
	if err != nil {
		t.Fatalf("WithProviderProtocol returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithModelID("gpt-5.3-codex")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpointName, err := endpointintent.ParseEndpointName("dev")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	pathRecord, err := buildPathRecord(context.Background(), "ex-tool-route", endpointName, providerConfig, decoded.Value.Delivery, decoded.Value.Request)
	if err != nil {
		t.Fatalf("buildPathRecord returned error: %v", err)
	}
	encoded, err := resolver.ProviderRequestDocumentEncoder(protocolkind.Responses).EncodeProviderRequestDocument(pathRecord.Request, pathRecord.ProviderDelivery, "ex-tool-route")
	if err != nil {
		t.Fatalf("EncodeProviderRequestDocument returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded.Value.RawBytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal provider payload returned error: %v", err)
	}
	if got := payload["model"]; got != "gpt-5.3-codex" {
		t.Fatalf("provider model = %#v, want gpt-5.3-codex", got)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("provider tools = %#v, want one tool", payload["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("provider tool = %T, want object", tools[0])
	}
	if tool["type"] != "function" || tool["name"] != "exec_command" {
		t.Fatalf("provider tool = %#v, want function exec_command", tool)
	}
	if got, ok := payload["parallel_tool_calls"].(bool); !ok || got {
		t.Fatalf("parallel_tool_calls = %#v, want false", payload["parallel_tool_calls"])
	}
	if got := payload["tool_choice"]; got != "auto" {
		t.Fatalf("tool_choice = %#v, want auto", got)
	}
}

func TestResponsesRouteToProviderEncode_PreservesCustomToolSurface(t *testing.T) {
	resolver := codecresolver.NewRuntimeCodecResolver()
	clientCodec := resolver.ClientCodec(canonical.ClientFamilyResponses)
	clientRequest := []byte(`{
		"model":"client-model",
		"input":"edit a file",
		"tools":[{
			"type":"custom",
			"name":"apply_patch",
			"description":"edit files",
			"format":{"type":"grammar","syntax":"lark","definition":"start: PATCH\nPATCH: /.+/"}
		}]
	}`)
	decoded, err := clientCodec.DecodeClientRequest(carrier.NewCarrierDocument(
		carrier.StageClientRequestIn,
		protocolkind.Responses,
		"application/json",
		nil,
		clientRequest,
		carrier.Meta{},
	))
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}

	ref, err := endpointintent.ParseProviderConfigRef("primary")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("azure")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(ref, spec, "contact-8837-resource", "keychain")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithProviderProtocol("responses_stream")
	if err != nil {
		t.Fatalf("WithProviderProtocol returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithModelID("gpt-5.3-codex")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpointName, err := endpointintent.ParseEndpointName("dev")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	pathRecord, err := buildPathRecord(context.Background(), "ex-custom-tool-route", endpointName, providerConfig, decoded.Value.Delivery, decoded.Value.Request)
	if err != nil {
		t.Fatalf("buildPathRecord returned error: %v", err)
	}
	encoded, err := resolver.ProviderRequestDocumentEncoder(protocolkind.Responses).EncodeProviderRequestDocument(pathRecord.Request, pathRecord.ProviderDelivery, "ex-custom-tool-route")
	if err != nil {
		t.Fatalf("EncodeProviderRequestDocument returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded.Value.RawBytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal provider payload returned error: %v", err)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("provider tools = %#v, want one tool", payload["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("provider tool = %T, want object", tools[0])
	}
	if tool["type"] != "custom" || tool["name"] != "apply_patch" {
		t.Fatalf("provider custom tool = %#v, want custom apply_patch", tool)
	}
	if _, ok := tool["format"].(map[string]any); !ok {
		t.Fatalf("provider custom tool format = %#v, want object", tool["format"])
	}
}

func TestChatRouteToResponsesProviderEncode_PreservesInstructions(t *testing.T) {
	resolver := codecresolver.NewRuntimeCodecResolver()
	clientCodec := resolver.ClientCodec(canonical.ClientFamilyChatCompletions)
	clientRequest := []byte(`{
		"model":"client-model",
		"messages":[
			{"role":"system","content":"You are a coding agent."},
			{"role":"developer","content":"Use native tools for file edits."},
			{"role":"user","content":"inspect files"}
		],
		"tools":[{
			"type":"function",
			"function":{
				"name":"exec_command",
				"description":"run a command",
				"parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}
			}
		}]
	}`)
	decoded, err := clientCodec.DecodeClientRequest(carrier.NewCarrierDocument(
		carrier.StageClientRequestIn,
		protocolkind.ChatCompletions,
		"application/json",
		nil,
		clientRequest,
		carrier.Meta{},
	))
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}

	ref, err := endpointintent.ParseProviderConfigRef("primary")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("azure")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(ref, spec, "contact-8837-resource", "keychain")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithProviderProtocol("responses_stream")
	if err != nil {
		t.Fatalf("WithProviderProtocol returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithModelID("gpt-5.3-codex")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpointName, err := endpointintent.ParseEndpointName("dev")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	pathRecord, err := buildPathRecord(context.Background(), "ex-chat-instructions-route", endpointName, providerConfig, decoded.Value.Delivery, decoded.Value.Request)
	if err != nil {
		t.Fatalf("buildPathRecord returned error: %v", err)
	}
	encoded, err := resolver.ProviderRequestDocumentEncoder(protocolkind.Responses).EncodeProviderRequestDocument(pathRecord.Request, pathRecord.ProviderDelivery, "ex-chat-instructions-route")
	if err != nil {
		t.Fatalf("EncodeProviderRequestDocument returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded.Value.RawBytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal provider payload returned error: %v", err)
	}
	if got := payload["instructions"]; got != "You are a coding agent.\n\nUse native tools for file edits." {
		t.Fatalf("instructions = %#v, want preserved system/developer instructions", got)
	}
	input, ok := payload["input"].(string)
	if !ok || input != "inspect files" {
		t.Fatalf("input = %#v, want user request only", payload["input"])
	}
}

// ---- continuation (adversarial) -------------------------------------------------
//
// These tests prove that the continuation pipeline:
//   1. Prepares continuation per-attempt (materializes history on fallback)
//   2. Preserves native continuation for responses-native targets
//   3. Fails closed on unsafe native replay divergence
//   4. Captures the completed record exactly once per successful response
//   5. Passes through cleanly when no store is wired

// TestContinuation_NilStore_Passthrough proves the pipeline completes
// successfully when no continuation store is wired.
func TestContinuation_NilStore_Passthrough(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a"}, "backend-a")

	var capturedReq canonical.CanonicalRequest
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		capturedReq = req.Request
		return carrier.NewCarrierDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"resp_ok","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	})

	ingress := RequestIngress{runner: runner}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "nil-store-1",
		Request:         newTransportRequestWithTurn(http.MethodPost, "/responses", "nonexistent", map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "hello"}}}),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("nil store should passthrough: %v", err)
	}
	if capturedReq.Turn().IsZero() {
		t.Fatal("nil store should preserve turn reference (fail-open for 400 downstream)")
	}
}

// TestContinuation_NativeContinuation_Preserved proves that for a
// responses-native target the turn reference is kept when the current
// request is a strict super-set of the stored chain.
func TestContinuation_NativeContinuation_Preserved(t *testing.T) {
	store := turnstate.NewMemoryContinuationStore()
	prevID := canonical.NewContinuationID("resp_prev")
	_ = store.Put(context.Background(), canonical.ContinuationRecord{
		ID:      prevID,
		RouteID: "alpha",
		ModelID: "m",
		RequestDelta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "first-turn"),
			},
		}),
		// Minimal stored response so chain length is 1 user item.
		// Current thread [user:first-turn, assistant:reply, user:second-turn]
		// is a strict superset (prefixLen == 1 == len(anchor)).
		Response: canonical.NewOutputWithUsage(
			canonical.SemanticKindConversation,
			"resp_prev",
			"m",
			[]canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "assistant-reply")},
			"stop",
			canonical.TokenUsage{},
		),
		Status: canonical.ContinuationStatusCompleted,
	})

	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a"}, "backend-a")

	var capturedReq canonical.CanonicalRequest
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		capturedReq = req.Request
		return carrier.NewCarrierDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"native-ok","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	})

	ingress := RequestIngress{runner: runner.WithContinuationStore(store)}

	// Exact match to stored chain + one new user item = strict superset -> native turn kept.
	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "native-1",
		Request:         newTransportRequestWithTurn(http.MethodPost, "/responses", "resp_prev", map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "first-turn"}, {"role": "assistant", "content": "assistant-reply"}, {"role": "user", "content": "second-turn"}}}),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedReq.Turn().IsZero() {
		t.Fatal("native continuation: turn should be preserved when current thread is superset of chain")
	}

	// Continuation record should be captured for the new response.
	rec, ok, err := store.Get(context.Background(), canonical.NewContinuationID("native-1_result"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("continuation record was not captured")
	}
	if rec.Status != canonical.ContinuationStatusCompleted {
		t.Fatalf("record status = %q, want completed", rec.Status)
	}
}

// TestContinuation_ResponsesRoute_PreservesProviderModelAndInheritedBands
// proves that the routed provider request keeps the selected provider model
// and inherited tool grammar even when the current turn only sends new input.
func TestContinuation_ResponsesRoute_PreservesProviderModelAndInheritedBands(t *testing.T) {
	endpointName, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	providerRef, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	providerSpec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(providerRef, providerSpec, "https://example.test/v1", "")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithProviderProtocol("responses")
	if err != nil {
		t.Fatalf("WithProviderProtocol returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithModelID("provider-model")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(endpointName, []endpointintent.ProviderConfig{providerConfig}, providerRef)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}

	maxTokens := 64
	temperature := 0.2
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{
		MaxOutputTokens: &maxTokens,
		Temperature:     &temperature,
		StopSequences:   []string{"DONE"},
	})
	if err != nil {
		t.Fatalf("NewGenerationControls returned error: %v", err)
	}
	outputFormat, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:        canonical.OutputFormatJSONSchema,
		Name:        "continuation_reply",
		Description: "structured continuation reply",
		Schema:      canonical.NewRawJSONObject(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"],"additionalProperties":false}`),
		Strict:      true,
	})
	if err != nil {
		t.Fatalf("NewOutputFormat returned error: %v", err)
	}

	store := turnstate.NewMemoryContinuationStore()
	prevID := canonical.NewContinuationID("resp_prev")
	if err := store.Put(context.Background(), canonical.ContinuationRecord{
		ID:      prevID,
		RouteID: "alpha",
		ModelID: "provider-model",
		RequestDelta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model:        "provider-model",
			Instructions: "Use native tools for filesystem work.",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "previous-turn"),
			},
			Tools: []canonical.ToolDecl{
				canonical.NewFunctionToolDecl("tool_1", "exec_command", "run a command", canonical.NewToolSchemaObject(`{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}`)),
			},
			ToolPolicy:    canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
			ToolCallBatch: canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne),
			Controls:      controls,
			OutputFormat:  outputFormat,
		}),
		Response: canonical.NewConversationOutput(
			"resp_prev",
			"provider-model",
			[]canonical.OutputItem{canonical.NewTextOutputItem("text_0", "previous-response")},
			"completed",
		),
		Status: canonical.ContinuationStatusCompleted,
	}); err != nil {
		t.Fatalf("store.Put returned error: %v", err)
	}

	var capturedReq canonical.CanonicalRequest
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		capturedReq = req.Request
		return carrier.NewCarrierDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"resp_2","model":"provider-model","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	})
	ingress := RequestIngress{runner: runner.WithContinuationStore(store)}

	_, err = ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "continuation-route-1",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"client-model","previous_response_id":"resp_prev","messages":[{"role":"user","content":"continue"}]}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("HandleRequestWithEndpoint returned error: %v", err)
	}

	if got := capturedReq.Model(); got != "provider-model" {
		t.Fatalf("provider request model = %q, want provider-model", got)
	}
	if got := capturedReq.Instructions(); got != "Use native tools for filesystem work." {
		t.Fatalf("provider request instructions = %q, want inherited instructions", got)
	}
	if got := len(capturedReq.Tools()); got != 1 {
		t.Fatalf("provider request tool count = %d, want 1", got)
	}
	if got := capturedReq.Tools()[0].ToolName(); got != "exec_command" {
		t.Fatalf("provider request tool name = %q, want exec_command", got)
	}
	if got := capturedReq.ToolPolicy(); got.Mode != canonical.ToolPolicyRequired {
		t.Fatalf("provider request tool policy = %q, want required", got.Mode)
	}
	if got := capturedReq.ToolCallBatch(); got.Mode != canonical.ToolCallBatchAtMostOne {
		t.Fatalf("provider request tool call batch = %q, want at_most_one", got.Mode)
	}
	if got := capturedReq.Turn().IsZero(); got {
		t.Fatal("provider request turn should be preserved for safe Responses continuation")
	}
}

// TestContinuation_Fallback_MaterializesHistory proves that when the first
// backend fails and the exchange falls back to a second backend with a
// different protocol, the stored continuation history is materialized into the
// outgoing request.
func TestContinuation_Fallback_MaterializesHistory(t *testing.T) {
	store := turnstate.NewMemoryContinuationStore()
	prevID := canonical.NewContinuationID("resp_prev")
	_ = store.Put(context.Background(), canonical.ContinuationRecord{
		ID:      prevID,
		RouteID: "alpha",
		ModelID: "m",
		RequestDelta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "previous-turn"),
			},
		}),
		Response: canonical.NewOutputWithUsage(
			canonical.SemanticKindConversation,
			"resp_prev",
			"m",
			[]canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "prev-response")},
			"stop",
			canonical.TokenUsage{},
		),
		Status: canonical.ContinuationStatusCompleted,
	})

	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls []string
	var capturedReqs []canonical.CanonicalRequest
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls = append(calls, req.Target.BackendRef)
		capturedReqs = append(capturedReqs, req.Request)
		if len(calls) == 1 {
			return nil, canonical.NewBackendError(req.Target.BackendRef, http.StatusServiceUnavailable, "down", "")
		}
		return carrier.NewCarrierDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"fallback-ok","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	})

	ingress := RequestIngress{runner: runner.WithContinuationStore(store)}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "fallback-1",
		Request:         newTransportRequestWithTurn(http.MethodPost, "/responses", "resp_prev", map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "current-turn"}}}),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(calls))
	}
	if calls[0] != "backend-a" || calls[1] != "backend-b" {
		t.Fatalf("unexpected call order: %v", calls)
	}

	// Second attempt materialized history = previous-turn + assistant-reply + current-turn.
	secondReq := capturedReqs[1]
	items := secondReq.Items()
	if len(items) < 2 {
		t.Fatalf("fallback: expected >=2 items, got %d", len(items))
	}
	found := map[string]bool{}
	for _, it := range items {
		found[it.Text] = true
	}
	if !found["previous-turn"] {
		t.Fatal("fallback: missing previous-turn")
	}
	if !found["current-turn"] {
		t.Fatal("fallback: missing current-turn")
	}

	// New record captured.
	rec, ok, err := store.Get(context.Background(), canonical.NewContinuationID("fallback-1_result"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("continuation record was not captured after fallback success")
	}
	if rec.RouteID != "alpha" {
		t.Fatalf("record routeID = %q, want alpha", rec.RouteID)
	}
}

// TestContinuation_UnsafeNativeReplay_FailsClosed proves that when a
// Responses target is selected but the current request diverges from the
// stored chain, the router emits a 400-style error (UnsafeNativeReplayError)
// instead of silently materializing a divergent thread.
func TestContinuation_UnsafeNativeReplay_FailsClosed(t *testing.T) {
	store := turnstate.NewMemoryContinuationStore()
	prevID := canonical.NewContinuationID("resp_prev")
	_ = store.Put(context.Background(), canonical.ContinuationRecord{
		ID:      prevID,
		RouteID: "alpha",
		ModelID: "m",
		RequestDelta: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "m",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "stored-turn"),
			},
		}),
		Response: canonical.NewOutputWithUsage(
			canonical.SemanticKindConversation,
			"resp_prev",
			"m",
			[]canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorAssistant, "stored-response")},
			"stop",
			canonical.TokenUsage{},
		),
		Status: canonical.ContinuationStatusCompleted,
	})

	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a"}, "backend-a")

	var providerCalled bool
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		providerCalled = true
		return nil, canonical.InternalError("should not reach provider")
	})

	ingress := RequestIngress{runner: runner.WithContinuationStore(store)}

	// Divergent request: overlapping but not prefix-equal to stored chain.
	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "unsafe-replay-1",
		Request:         newTransportRequestWithTurn(http.MethodPost, "/responses", "resp_prev", map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "stored-turn"}, {"role": "user", "content": "divergent-turn"}}}),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if providerCalled {
		t.Fatal("provider should not be called for unsafe native replay")
	}
	if err == nil {
		t.Fatal("expected error for unsafe native replay, got nil")
	}
	// UnsafeNativeReplayError unwraps to a BadRequest.
	if !strings.Contains(err.Error(), "400") && !strings.Contains(strings.ToLower(err.Error()), "unsafe") {
		t.Fatalf("expected UnsafeNativeReplayError (400), got: %v", err)
	}
}

// TestContinuation_NoDuplicate_PersistsOnceOnRetry proves that even when
// multiple fallback attempts occur, the continuation record is only persisted
// once — on the first successful response.
func TestContinuation_NoDuplicate_PersistsOnceOnRetry(t *testing.T) {
	store := turnstate.NewMemoryContinuationStore()

	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls int
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		calls++
		if calls == 1 {
			return nil, canonical.NewBackendError("backend-a", http.StatusServiceUnavailable, "down", "")
		}
		return carrier.NewCarrierDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"retry-ok","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	})

	ingress := RequestIngress{runner: runner.WithContinuationStore(store)}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "no-dup-1",
		Request:         newTransportRequestWithTurn(http.MethodPost, "/responses", "", map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "hi"}}}),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if calls != 2 {
		t.Fatalf("expected 2 provider calls, got %d", calls)
	}

	rec, ok, err := store.Get(context.Background(), canonical.NewContinuationID("no-dup-1_result"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected continuation record to be persisted")
	}
	if rec.Status != canonical.ContinuationStatusCompleted {
		t.Fatalf("record status = %q, want completed", rec.Status)
	}

	// Chain should contain only the single new record (no duplicates).
	chain, err := store.Chain(context.Background(), canonical.NewContinuationID("no-dup-1_result"))
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 1 {
		t.Fatalf("expected chain length 1, got %d", len(chain))
	}
}
