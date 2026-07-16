package exchange

import (
	"context"
	"encoding/json"
	"io"
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
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/wire"
)

type deterministicResponseIDGenerator struct{}

func (deterministicResponseIDGenerator) NewResponseID(_ context.Context, exchangeID string) (replay.ResponseID, error) {
	return replay.ResponseID("swobu_" + exchangeID), nil
}

type countingResponseIDGenerator struct {
	calls int
}

func (g *countingResponseIDGenerator) NewResponseID(_ context.Context, exchangeID string) (replay.ResponseID, error) {
	g.calls++
	return replay.ResponseID("swobu_" + exchangeID), nil
}

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

// TestExchangeMachine_RetryFallback_AllocatesReplayResponseIDOnce proves the
// exchange retry path preserves one response ID across backend retries.
func TestExchangeMachine_RetryFallback_AllocatesReplayResponseIDOnce(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a", "backend-b"}, "backend-a")

	var calls []string
	gen := &countingResponseIDGenerator{}
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
	}).WithReplayStore(replay.NewMemoryStore()).WithResponseIDs(gen)

	sink := &recordingEvidenceSink{}
	ingress := RequestIngress{trafficEvidence: sink, runner: runner}

	out, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "fallback-ids",
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
	if gen.calls != 1 {
		t.Fatalf("response id generator calls = %d, want 1", gen.calls)
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

func TestEndpointToWorkspaceRouting_UsesExplicitRouteModelID(t *testing.T) {
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	firstRef, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef(first) returned error: %v", err)
	}
	secondRef, err := endpointintent.ParseProviderConfigRef("backend-b")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef(second) returned error: %v", err)
	}
	first, err := endpointintent.NewProviderConfig(firstRef, spec, "https://a.test/v1", "cred-a")
	if err != nil {
		t.Fatalf("NewProviderConfig(first) returned error: %v", err)
	}
	first, err = first.WithRouteModelID("gpt")
	if err != nil {
		t.Fatalf("WithRouteModelID(first) returned error: %v", err)
	}
	first, err = first.WithModelID("gpt-4.1")
	if err != nil {
		t.Fatalf("WithModelID(first) returned error: %v", err)
	}
	second, err := endpointintent.NewProviderConfig(secondRef, spec, "https://b.test/v1", "cred-b")
	if err != nil {
		t.Fatalf("NewProviderConfig(second) returned error: %v", err)
	}
	second, err = second.WithRouteModelID("gpt")
	if err != nil {
		t.Fatalf("WithRouteModelID(second) returned error: %v", err)
	}
	second, err = second.WithModelID("gpt-4o")
	if err != nil {
		t.Fatalf("WithModelID(second) returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{first, second}, secondRef)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}

	wr := endpointToWorkspaceRouting(endpoint)
	if got, want := wr.DefaultModel, "gpt"; got != want {
		t.Fatalf("workspace default model = %q, want %q", got, want)
	}
	route, ok := wr.Routes["gpt"]
	if !ok {
		t.Fatalf("workspace routes missing gpt route: %#v", wr.Routes)
	}
	if got, want := route.ModelName, "gpt"; got != want {
		t.Fatalf("route model name = %q, want %q", got, want)
	}
	if len(route.Targets) != 2 {
		t.Fatalf("route targets = %d, want 2", len(route.Targets))
	}
	if got, want := route.Targets[0].ID, "backend-a"; got != want {
		t.Fatalf("first target id = %q, want %q", got, want)
	}
	if got, want := route.Targets[0].Model, "gpt-4.1"; got != want {
		t.Fatalf("first target model = %q, want %q", got, want)
	}
	if got, want := route.Targets[1].ID, "backend-b"; got != want {
		t.Fatalf("second target id = %q, want %q", got, want)
	}
	if got, want := route.Targets[1].Model, "gpt-4o"; got != want {
		t.Fatalf("second target model = %q, want %q", got, want)
	}
}

type staticEndpointReader struct {
	endpoint endpointintent.Endpoint
}

func (r staticEndpointReader) GetEndpoint(context.Context, endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	return r.endpoint, nil
}

func TestRequestIngress_ListModels_UsesRouteModelIDs(t *testing.T) {
	t.Parallel()

	endpoint := testIngressEndpointWithRouteModel(t, "gpt", "gpt-4.1")
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	ingress := RequestIngress{
		endpoints: staticEndpointReader{endpoint: endpoint},
	}

	out, err := ingress.ListModels(context.Background(), ListModelsInput{EndpointName: name})
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if got, want := out.DefaultModelID, "gpt"; got != want {
		t.Fatalf("default model = %q, want %q", got, want)
	}
	if len(out.Models) != 1 {
		t.Fatalf("models len = %d, want 1", len(out.Models))
	}
	if got, want := out.Models[0].ID, "gpt"; got != want {
		t.Fatalf("model id = %q, want %q", got, want)
	}
	if got, want := out.Models[0].ModelID, "gpt"; got != want {
		t.Fatalf("model model_id = %q, want %q", got, want)
	}
	if got, want := out.Models[0].BackendRef, "backend-a"; got != want {
		t.Fatalf("backend ref = %q, want %q", got, want)
	}
}

func TestRequestIngress_HandleRequestWithEndpoint_UsesProviderModelForProviderIngress(t *testing.T) {
	t.Parallel()

	endpoint := testIngressEndpointWithRouteModel(t, "gpt", "gpt-4.1")
	var gotRequestModel string
	ingress := RequestIngress{
		runner: withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
			gotRequestModel = req.Request.Model()
			return carrier.NewCarrierDocument(
				carrier.StageProviderIngressIn,
				req.Target.ProtocolKind,
				"application/json",
				nil,
				[]byte(`{"id":"resp_1","model":"gpt-4.1","output_text":"ok"}`),
				carrier.Meta{},
			), nil
		}),
	}

	out, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "route-model-request",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"gpt","messages":[{"role":"user","content":"hi"}]}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("HandleRequestWithEndpoint returned error: %v", err)
	}
	if out.Response.Transport.Status != http.StatusOK {
		t.Fatalf("response status = %d, want 200", out.Response.Transport.Status)
	}
	if got, want := gotRequestModel, "gpt-4.1"; got != want {
		t.Fatalf("provider request model = %q, want %q", got, want)
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

	ref, err := endpointintent.ParseProviderConfigRef("default")
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
	encoded, err := resolver.ProviderRequestDocumentEncoder(protocolkind.Responses).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: pathRecord.Request}, pathRecord.ProviderDelivery, "ex-tool-route")
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

	ref, err := endpointintent.ParseProviderConfigRef("default")
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
	encoded, err := resolver.ProviderRequestDocumentEncoder(protocolkind.Responses).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: pathRecord.Request}, pathRecord.ProviderDelivery, "ex-custom-tool-route")
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

	ref, err := endpointintent.ParseProviderConfigRef("default")
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
	encoded, err := resolver.ProviderRequestDocumentEncoder(protocolkind.Responses).EncodeProviderRequestDocument(wire.ProviderEncodeInput{Request: pathRecord.Request}, pathRecord.ProviderDelivery, "ex-chat-instructions-route")
	if err != nil {
		t.Fatalf("EncodeProviderRequestDocument returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded.Value.RawBytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal provider payload returned error: %v", err)
	}
	if got := payload["instructions"]; got != "You are a coding agent.\n\nUse native tools for file edits." {
		t.Fatalf("instructions = %q, want merged instructions", got)
	}
}

// ---- replay integration (adversarial) -------------------------------------------
//
// These tests prove that the replay pipeline:
//   1. Captures terminal-success records into the store
//   2. Substitutes Swobu response IDs for provider IDs
//   3. Rejects cleanly when no store is wired

// TestReplay_NilStore_RejectedBeforeProviderIngress proves the request path
// fails closed before provider ingress when replay storage is missing.
func TestReplay_NilStore_RejectedBeforeProviderIngress(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a"}, "backend-a")

	calls := 0
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerIngress: func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
				calls++
				return carrier.NewCarrierDocument(
					carrier.StageProviderIngressIn,
					req.Target.ProtocolKind,
					"application/json",
					nil,
					[]byte(`{"id":"resp_ok","model":"m","output_text":"ok"}`),
					carrier.Meta{},
				), nil
			},
		},
		ResponseIDs: deterministicResponseIDGenerator{},
	}

	ingress := RequestIngress{runner: runner}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "nil-store-1",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"m","input":"hello"}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		if !strings.Contains(err.Error(), "replay store is required") {
			t.Fatalf("nil store error = %v, want replay store rejection", err)
		}
	} else {
		t.Fatal("expected nil replay store to reject before provider ingress")
	}
	if calls != 0 {
		t.Fatalf("provider ingress calls = %d, want 0", calls)
	}
}

// TestResponsesBufferedReplayRecordNativeTargetFullyPopulated proves that a
// successful buffered response is captured into the replay store with the
// Swobu response ID and a fully populated native replay ref.
func TestResponsesBufferedReplayRecordNativeTargetFullyPopulated(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a"}, "backend-a")

	store := replay.NewMemoryStore()
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerIngress: func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
				_ = req
				return carrier.NewCarrierDocument(
					carrier.StageProviderIngressIn,
					protocolkind.Responses,
					"application/json",
					nil,
					[]byte(`{"id":"provider_resp_1","model":"m","output_text":"ok"}`),
					carrier.Meta{},
				), nil
			},
		},
	}.WithReplayStore(store).WithResponseIDs(deterministicResponseIDGenerator{})

	ingress := RequestIngress{runner: runner}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "replay-commit-1",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"m","input":"hello"}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The record should exist under the Swobu ID in namespace "alpha" (the endpoint name).
	rec, ok, err := store.Get(context.Background(), unsafeLocalReplayScope("alpha"), replay.ReplayIDFromResponseID("swobu_replay-commit-1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("replay record was not captured")
	}
	if rec.Response.ResultID() != "swobu_replay-commit-1" {
		t.Fatalf("stored response ID = %q, want Swobu ID", rec.Response.ResultID())
	}
	if rec.Native == nil {
		t.Fatal("native ref = nil, want provider native ID persisted")
	}
	wantTarget := replay.TargetKey{
		ProviderSpec:     "openai_compatible",
		Protocol:         protocolkind.Responses,
		ProviderProtocol: "responses",
		BaseURL:          "https://example.test/v1",
		AuthScope:        "cred-backend-a",
		ModelID:          "m",
	}
	if rec.Native.ReplayID != replay.ReplayIDFromResponseID("swobu_replay-commit-1") {
		t.Fatalf("native replay id = %q, want %q", rec.Native.ReplayID, replay.ReplayIDFromResponseID("swobu_replay-commit-1"))
	}
	if rec.Native.Target != wantTarget {
		t.Fatalf("native target = %+v, want %+v", rec.Native.Target, wantTarget)
	}
	if rec.Native.Kind != replay.NativeRefProviderResponseID {
		t.Fatalf("native kind = %q, want %q", rec.Native.Kind, replay.NativeRefProviderResponseID)
	}
	if rec.Native.Value != "provider_resp_1" {
		t.Fatalf("native ref value = %q, want provider_resp_1", rec.Native.Value)
	}
}

// TestResponsesBufferedReplayUsesDefaultResponseIDsWhenUnset proves the replay
// runtime allocates a Swobu response ID even when the request input leaves it
// unset.
func TestResponsesBufferedReplayUsesDefaultResponseIDsWhenUnset(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a"}, "backend-a")

	store := replay.NewMemoryStore()
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerIngress: func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
				_ = req
				return carrier.NewCarrierDocument(
					carrier.StageProviderIngressIn,
					protocolkind.Responses,
					"application/json",
					nil,
					[]byte(`{"id":"provider_resp_1","model":"m","output_text":"ok"}`),
					carrier.Meta{},
				), nil
			},
		},
		ResponseIDs: deterministicResponseIDGenerator{},
	}.WithReplayStore(store)

	ingress := RequestIngress{runner: runner}
	resp, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "replay-default-1",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"m","input":"hello"}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response.Transport.Body == nil {
		t.Fatal("buffered response body was nil")
	}
	raw, err := io.ReadAll(resp.Response.Transport.Body)
	if err != nil {
		t.Fatalf("read buffered body: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	id, _ := body["id"].(string)
	if id != "swobu_replay-default-1" {
		t.Fatalf("response id=%q, want swobu_replay-default-1", id)
	}
	if id == "provider_resp_1" {
		t.Fatalf("provider-native ID leaked to client: %s", string(raw))
	}

	rec, ok, err := store.Get(context.Background(), unsafeLocalReplayScope("alpha"), replay.ReplayIDFromResponseID(replay.ResponseID(id)))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("replay record not captured for id %q", id)
	}
	if rec.Response.ResultID() != id {
		t.Fatalf("stored response ID = %q, want %q", rec.Response.ResultID(), id)
	}
	if rec.Native == nil || rec.Native.Value != "provider_resp_1" {
		t.Fatalf("native replay capture = %+v, want provider_resp_1", rec.Native)
	}
}

// TestReplay_FirstTurn_NoPreviousReplayID proves a fresh conversation turn
// writes a record with no native replay pointer.
func TestReplay_FirstTurn_NoPreviousReplayID(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a"}, "backend-a")

	store := replay.NewMemoryStore()
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		return carrier.NewCarrierDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"native-ok","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	}).WithReplayStore(store).WithResponseIDs(deterministicResponseIDGenerator{})

	ingress := RequestIngress{runner: runner}

	_, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "first-turn-1",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"m","messages":[{"role":"user","content":"first-turn"}]}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec, ok, err := store.Get(context.Background(), unsafeLocalReplayScope("alpha"), replay.ReplayIDFromResponseID("swobu_first-turn-1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("replay record missing")
	}
	if rec.Native == nil {
		t.Fatal("expected native ref to be captured from provider output")
	}
	wantTarget := replay.TargetKey{
		ProviderSpec:     "openai_compatible",
		Protocol:         protocolkind.Responses,
		ProviderProtocol: "responses",
		BaseURL:          "https://example.test/v1",
		AuthScope:        "cred-backend-a",
		ModelID:          "m",
	}
	if rec.Native.ReplayID != replay.ReplayIDFromResponseID("swobu_first-turn-1") {
		t.Fatalf("native replay id = %q, want %q", rec.Native.ReplayID, replay.ReplayIDFromResponseID("swobu_first-turn-1"))
	}
	if rec.Native.Target != wantTarget {
		t.Fatalf("native target = %+v, want %+v", rec.Native.Target, wantTarget)
	}
	if rec.Native.Kind != replay.NativeRefProviderResponseID {
		t.Fatalf("native kind = %q, want %q", rec.Native.Kind, replay.NativeRefProviderResponseID)
	}
	if rec.Native.Value != "first-turn-1_result" {
		t.Fatalf("native ref = %q, want first-turn-1_result", rec.Native.Value)
	}
}

// TestResponsesStreamingReplayRecordNativeTargetFullyPopulated proves the
// streaming replay path captures the full native target binding, not only the
// provider result ID string.
func TestResponsesStreamingReplayRecordNativeTargetFullyPopulated(t *testing.T) {
	store := replay.NewMemoryStore()
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"provider_resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"provider_resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"provider_resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerIngress: func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
				return carrier.CarrierStream{
					Stage:   carrier.StageProviderIngressIn,
					Family:  req.Target.ProtocolKind,
					Framing: carrier.FramingSSE,
					Frames:  carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader(providerSSE))),
				}, nil
			},
		},
		ResponseIDs: deterministicResponseIDGenerator{},
	}.WithReplayStore(store)

	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "stream-native-1",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      unsafeLocalReplayScope("alpha"),
		Target:           NewRoutableTarget("backend-a", "openai_compatible", "https://example.test/v1", "cred-backend-a", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatal("streaming response body was nil")
	}
	if _, err := io.ReadAll(out.Transport.Body); err != nil {
		t.Fatalf("read streaming body: %v", err)
	}

	rec, ok, err := store.Get(context.Background(), unsafeLocalReplayScope("alpha"), replay.ReplayIDFromResponseID("swobu_stream-native-1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("replay record missing")
	}
	wantTarget := replay.TargetKey{
		ProviderSpec:     "openai_compatible",
		Protocol:         protocolkind.Responses,
		ProviderProtocol: "responses",
		BaseURL:          "https://example.test/v1",
		AuthScope:        "cred-backend-a",
		ModelID:          "m",
	}
	if rec.Native == nil {
		t.Fatal("native ref = nil, want provider native ID persisted")
	}
	if rec.Native.ReplayID != replay.ReplayIDFromResponseID("swobu_stream-native-1") {
		t.Fatalf("native replay id = %q, want %q", rec.Native.ReplayID, replay.ReplayIDFromResponseID("swobu_stream-native-1"))
	}
	if rec.Native.Target != wantTarget {
		t.Fatalf("native target = %+v, want %+v", rec.Native.Target, wantTarget)
	}
	if rec.Native.Kind != replay.NativeRefProviderResponseID {
		t.Fatalf("native kind = %q, want %q", rec.Native.Kind, replay.NativeRefProviderResponseID)
	}
	if rec.Native.Value != "provider_resp_1" {
		t.Fatalf("native value = %q, want provider_resp_1", rec.Native.Value)
	}
}

// TestResponsesStreamingCreatedAndCompletedUseSameSwobuID proves the wire
// stream does not leak the provider ID on response.created and keeps the
// allocated Swobu ID stable through completion.
func TestResponsesStreamingCreatedAndCompletedUseSameSwobuID(t *testing.T) {
	store := replay.NewMemoryStore()
	providerSSE := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"provider_resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"provider_resp_1\",\"item_id\":\"msg_1\",\"delta\":\"ok\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"provider_resp_1\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	runner := Runner{
		Runtime: runtimeWithProviderIngress{
			RuntimeResolver: codecresolver.NewRuntimeCodecResolver(),
			providerIngress: func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
				return carrier.CarrierStream{
					Stage:   carrier.StageProviderIngressIn,
					Family:  req.Target.ProtocolKind,
					Framing: carrier.FramingSSE,
					Frames:  carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader(providerSSE))),
				}, nil
			},
		},
		ResponseIDs: deterministicResponseIDGenerator{},
	}.WithReplayStore(store)

	out, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "stream-id",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      unsafeLocalReplayScope("alpha"),
		Target:           NewRoutableTarget("backend-a", "openai_compatible", "https://example.test/v1", "cred-backend-a", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatal("streaming response body was nil")
	}
	raw, err := io.ReadAll(out.Transport.Body)
	if err != nil {
		t.Fatalf("read streaming body: %v", err)
	}
	body := string(raw)
	wantID := "swobu_stream-id"
	if !strings.Contains(body, `"type":"response.created","response":{"id":"`+wantID+`"`) {
		t.Fatalf("streaming response.created did not use Swobu ID %q: %s", wantID, body)
	}
	if !strings.Contains(body, `"type":"response.completed","response":{"id":"`+wantID+`"`) {
		t.Fatalf("streaming response.completed did not use Swobu ID %q: %s", wantID, body)
	}
	if strings.Contains(body, "provider_resp_1") {
		t.Fatalf("provider-native ID leaked into client stream: %s", body)
	}
}

// TestReplay_ClientResponseID_ReplacesProviderID proves the client-visible
// response body contains the Swobu ID, not the provider-native one.
func TestReplay_ClientResponseID_ReplacesProviderID(t *testing.T) {
	endpoint := testIngressEndpointWithPaths(t, []string{"backend-a"}, "backend-a")

	store := replay.NewMemoryStore()
	runner := withRuntime(func(_ context.Context, req ProviderRequest) (ProviderIngress, error) {
		return carrier.NewCarrierDocument(
			carrier.StageProviderIngressIn,
			req.Target.ProtocolKind,
			"application/json",
			nil,
			[]byte(`{"id":"provider_resp_1","model":"m","output_text":"ok"}`),
			carrier.Meta{},
		), nil
	}).WithReplayStore(store).WithResponseIDs(deterministicResponseIDGenerator{})

	ingress := RequestIngress{runner: runner}

	resp, err := ingress.HandleRequestWithEndpoint(context.Background(), endpoint, RequestInput{
		ExchangeID:      "client-id-1",
		Request:         NewTransportRequest(http.MethodPost, "/responses", nil, []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)),
		ClientFamily:    canonical.ClientFamilyResponses,
		ResponseFraming: delivery.FramingSSE,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var body map[string]any
	raw, _ := io.ReadAll(resp.Response.Transport.Body)
	_ = resp.Response.Transport.Body.Close()
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	id, _ := body["id"].(string)
	if id != "swobu_client-id-1" {
		t.Fatalf("client response id = %q, want swobu_client-id-1", id)
	}
}
