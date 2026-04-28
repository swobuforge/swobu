package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/metrofun/swobu/internal/adapters/outbound/continuitystore"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/domain/runtimeevidence"
	"github.com/metrofun/swobu/internal/ports"
)

func TestRequestPathHandler_ResolvesSelectedTargetExecutesProviderAndEmitsEvidence(t *testing.T) {
	endpoint := testEndpoint(t)
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"chatcmpl_1",
				"m",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				},
				"stop",
			),
		),
	}
	evidence := &capturingEvidenceSink{}
	orchestrator := requestpath.NewRequestHandler(reader, provider, evidence, nil)

	out, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-1",
		Request: compatibility.NewDialogRequest(
			"m",
			[]compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")},
		),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("HandleRequest returned error: %v", err)
	}
	if got := provider.got.Target.BackendRef; got != "backend-a" {
		t.Fatalf("backend ref = %q, want %q", got, "backend-a")
	}
	if got := provider.got.Target.ProtocolKind; got != "chat_completions" {
		t.Fatalf("protocol kind = %q, want %q", got, "chat_completions")
	}
	if got := out.Target.BackendRef; got != "backend-a" {
		t.Fatalf("output target backend = %q, want %q", got, "backend-a")
	}
	if len(evidence.events) != 2 {
		t.Fatalf("events = %d, want 2", len(evidence.events))
	}
	if got := evidence.events[0].RequestID().String(); got != "req-1" {
		t.Fatalf("accepted request id = %q, want %q", got, "req-1")
	}
	if got := evidence.events[0].Result(); got != runtimeevidence.ResultClassInProgress {
		t.Fatalf("accepted result class = %q, want %q", got, runtimeevidence.ResultClassInProgress)
	}
	if got := evidence.events[1].Result(); got != runtimeevidence.ResultClassSuccess {
		t.Fatalf("finished result class = %q, want %q", got, runtimeevidence.ResultClassSuccess)
	}
	if got := evidence.events[1].StatusCode(); got != 200 {
		t.Fatalf("status code = %d, want %d", got, 200)
	}
	typed, ok := out.Response.Output().(compatibility.CanonicalOutputValue)
	if !ok {
		t.Fatalf("output type = %T, want compatibility.CanonicalOutputValue", out.Response.Output())
	}
	if got := typed.Text(); got != "ok" {
		t.Fatalf("output text = %q, want %q", got, "ok")
	}
}

func TestRequestPathHandler_RespectsClientModelWhenConfigured(t *testing.T) {
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	refA, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	refB, err := endpointintent.ParseProviderConfigRef("backend-b")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	cfgA, err := endpointintent.NewProviderConfig(refA, spec, "https://a.test/v1", "cred-a", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfgB, err := endpointintent.NewProviderConfig(refB, spec, "https://b.test/v1", "cred-b", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfgA, err = cfgA.WithModelID("model-default")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	cfgB, err = cfgB.WithModelID("model-client")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfgA, cfgB}, refA)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"chatcmpl_1",
				"model-client",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				},
				"stop",
			),
		),
	}
	orchestrator := requestpath.NewRequestHandler(reader, provider, nil, nil)

	out, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-model",
		Request: compatibility.NewDialogRequest(
			requestpath.CanonicalModelAlias("custom", "model-client"),
			[]compatibility.CanonicalItem{
				compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
			},
		),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	got, ok := provider.got.Request.(compatibility.DialogCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want compatibility.DialogCanonicalRequest", provider.got.Request)
	}
	if got.Model() != "model-client" {
		t.Fatalf("forwarded model = %q, want %q", got.Model(), "model-client")
	}
	if provider.got.Target.BackendRef != "backend-b" {
		t.Fatalf("backend ref = %q, want %q", provider.got.Target.BackendRef, "backend-b")
	}
	if out.Response.Metadata().ModelResolutionMode != "client" {
		t.Fatalf("resolution mode = %q, want client", out.Response.Metadata().ModelResolutionMode)
	}
}

func TestRequestPathHandler_FallsBackToSelectedTargetWhenClientModelUnknown(t *testing.T) {
	endpoint := testEndpoint(t)
	selected := endpoint.SelectedProviderConfig()
	selected, err := selected.WithModelID("model-from-swobu")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpoint, err = endpointintent.NewEndpoint(
		endpoint.Name(),
		[]endpointintent.ProviderConfig{selected},
		selected.Ref(),
	)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"chatcmpl_1",
				"model-from-swobu",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				},
				"stop",
			),
		),
	}
	orchestrator := requestpath.NewRequestHandler(reader, provider, nil, nil)

	out, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-model",
		Request: compatibility.NewDialogRequest(
			"client-default-model",
			[]compatibility.CanonicalItem{
				compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
			},
		),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	got, ok := provider.got.Request.(compatibility.DialogCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want compatibility.DialogCanonicalRequest", provider.got.Request)
	}
	if got.Model() != "model-from-swobu" {
		t.Fatalf("forwarded model = %q, want %q", got.Model(), "model-from-swobu")
	}
	if provider.got.Target.BackendRef != selected.Ref().String() {
		t.Fatalf("backend ref = %q, want %q", provider.got.Target.BackendRef, selected.Ref().String())
	}
	if out.Response.Metadata().ModelResolutionMode != "default_unknown" {
		t.Fatalf("resolution mode = %q, want default_unknown", out.Response.Metadata().ModelResolutionMode)
	}
}

func TestRequestPathHandler_ProviderModelLiteralSelectsConfiguredTarget(t *testing.T) {
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	refA, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	refB, err := endpointintent.ParseProviderConfigRef("backend-b")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	cfgA, err := endpointintent.NewProviderConfig(refA, spec, "https://a.test/v1", "cred-a", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfgB, err := endpointintent.NewProviderConfig(refB, spec, "https://b.test/v1", "cred-b", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfgA, err = cfgA.WithModelID("model-default")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	cfgB, err = cfgB.WithModelID("model-client")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfgA, cfgB}, refA)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"chatcmpl_1",
				"model-default",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				},
				"stop",
			),
		),
	}
	orchestrator := requestpath.NewRequestHandler(reader, provider, nil, nil)

	out, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-model",
		Request: compatibility.NewDialogRequest(
			"custom:model-client",
			[]compatibility.CanonicalItem{
				compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
			},
		),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	got, ok := provider.got.Request.(compatibility.DialogCanonicalRequest)
	if !ok {
		t.Fatalf("request type = %T, want compatibility.DialogCanonicalRequest", provider.got.Request)
	}
	if got.Model() != "model-client" {
		t.Fatalf("forwarded model = %q, want %q", got.Model(), "model-client")
	}
	if provider.got.Target.BackendRef != "backend-b" {
		t.Fatalf("backend ref = %q, want %q", provider.got.Target.BackendRef, "backend-b")
	}
	if out.Response.Metadata().ModelResolutionMode != "client" {
		t.Fatalf("resolution mode = %q, want client", out.Response.Metadata().ModelResolutionMode)
	}
}

func TestRequestPathHandler_RejectsClientModelWhenSelectedProviderModelIsUnset(t *testing.T) {
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	config, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "cred-1", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{config}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"chatcmpl_1",
				"client-model",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				},
				"stop",
			),
		),
	}
	orchestrator := requestpath.NewRequestHandler(reader, provider, nil, nil)

	_, err = orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-model-unset",
		Request: compatibility.NewDialogRequest(
			"",
			[]compatibility.CanonicalItem{
				compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
			},
		),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err == nil {
		t.Fatal("Handle returned nil error, want selected-provider-model failure")
	}
	var typed compatibility.Error
	if !errors.As(err, &typed) {
		t.Fatalf("error type = %T, want compatibility.Error", err)
	}
	if typed.Code != compatibility.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", typed.Code, compatibility.ErrorCodeBadRequest)
	}
	if typed.Message != "selected provider model is not configured" {
		t.Fatalf("error message = %q, want %q", typed.Message, "selected provider model is not configured")
	}
	if provider.got.Request != nil {
		t.Fatalf("provider execute should not be called, got request type %T", provider.got.Request)
	}
}

func TestRequestPathHandler_ListModelsReturnsOneIDPerModel(t *testing.T) {
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	refA, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	refB, err := endpointintent.ParseProviderConfigRef("backend-b")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	cfgA, err := endpointintent.NewProviderConfig(refA, spec, "https://a.test/v1", "cred-a", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfgB, err := endpointintent.NewProviderConfig(refB, spec, "https://b.test/v1", "cred-b", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	cfgA, err = cfgA.WithModelID("shared-model")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	cfgB, err = cfgB.WithModelID("shared-model")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfgA, cfgB}, refA)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	orchestrator := requestpath.NewRequestHandler(fakeEndpointReader{endpoint: endpoint}, &fakeProviderExecutor{}, nil, nil)

	out, err := orchestrator.ListModels(context.Background(), requestpath.ListModelsInput{EndpointName: endpoint.Name()})
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if out.DefaultModelID != "custom:shared-model:backend-a" {
		t.Fatalf("default model id = %q", out.DefaultModelID)
	}
	if len(out.Models) != 3 {
		t.Fatalf("models len = %d, want 3", len(out.Models))
	}
	seen := map[string]struct{}{}
	for _, model := range out.Models {
		if _, exists := seen[model.ID]; exists {
			t.Fatalf("model ids must be unique: %q", model.ID)
		}
		seen[model.ID] = struct{}{}
	}
	if _, ok := seen[compatibility.PrimaryTargetSelector]; !ok {
		t.Fatalf("models must include %q alias", compatibility.PrimaryTargetSelector)
	}
}

func TestRequestPathHandler_ListModelsOmitsProviderConfigsWithoutBackendModelID(t *testing.T) {
	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	cfg, err := endpointintent.NewProviderConfig(ref, spec, "https://a.test/v1", "cred-a", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{cfg}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	orchestrator := requestpath.NewRequestHandler(fakeEndpointReader{endpoint: endpoint}, &fakeProviderExecutor{}, nil, nil)

	out, err := orchestrator.ListModels(context.Background(), requestpath.ListModelsInput{EndpointName: endpoint.Name()})
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(out.Models) != 0 {
		t.Fatalf("models len = %d, want 0", len(out.Models))
	}
	if got := out.DefaultModelID; got != "" {
		t.Fatalf("default model id = %q, want empty when selected provider model is unset", got)
	}
}

func TestRequestPathHandler_RehydratesResponsesContinuityForChatTargets(t *testing.T) {
	endpoint := testEndpointWithProtocol(t, protocolsurface.ChatCompletions)
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"chatcmpl_1",
				"m",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "done"),
				},
				"stop",
			),
		),
	}
	continuity := &fakeContinuityStore{
		byResponseID: map[string]compatibility.ContinuitySnapshot{
			"resp_prev": compatibility.NewContinuitySnapshot("resp_prev", "m", []compatibility.CanonicalItem{
				compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
				compatibility.NewTextItem(compatibility.ItemAuthorAssistant, "hello"),
			}),
		},
	}
	orchestrator := requestpath.NewRequestHandler(reader, provider, nil, continuity)

	out, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-2",
		Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:              "m",
			PreviousResponseID: "resp_prev",
			InputText:          "continue",
		}),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("HandleRequest returned error: %v", err)
	}

	typed, ok := provider.got.Request.(compatibility.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("provider request type = %T, want compatibility.GenerationCanonicalRequest", provider.got.Request)
	}
	thread := typed.Thread()
	if len(thread) != 3 {
		t.Fatalf("thread len = %d, want 3", len(thread))
	}
	if got := thread[1].Text; got != "hello" {
		t.Fatalf("latest prior message text = %q, want %q", got, "hello")
	}
	lastTurn := typed.LastTurn()
	if len(lastTurn) != 1 {
		t.Fatalf("last turn len = %d, want 1", len(lastTurn))
	}
	if got := lastTurn[0].Text; got != "continue" {
		t.Fatalf("last turn text = %q, want %q", got, "continue")
	}
	if got := typed.PreviousResponseID(); got != "resp_prev" {
		t.Fatalf("previous response id = %q, want %q", got, "resp_prev")
	}
	if len(continuity.stored) != 1 {
		t.Fatalf("stored snapshots = %d, want 1", len(continuity.stored))
	}
	if got := continuity.stored[0].ResponseID; got != "chatcmpl_1" {
		t.Fatalf("stored response id = %q, want %q", got, "chatcmpl_1")
	}
	if got := continuity.stored[0].Thread[len(continuity.stored[0].Thread)-1].Text; got != "done" {
		t.Fatalf("stored assistant text = %q, want %q", got, "done")
	}
	output, ok := out.Response.Output().(compatibility.CanonicalOutputValue)
	if !ok {
		t.Fatalf("output type = %T, want compatibility.CanonicalOutputValue", out.Response.Output())
	}
	if got := output.ResultID(); got != "chatcmpl_1" {
		t.Fatalf("output result id = %q, want %q", got, "chatcmpl_1")
	}
}

func TestRequestPathHandler_RealizesConversationRequestsOntoResponsesTargets(t *testing.T) {
	endpoint := testEndpointWithProtocol(t, protocolsurface.Responses)
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"resp_1",
				"m",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				},
				"completed",
			),
		),
	}
	orchestrator := requestpath.NewRequestHandler(reader, provider, nil, &fakeContinuityStore{})

	_, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-3",
		Request: compatibility.NewDialogRequest(
			"m",
			[]compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")},
		),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("HandleRequest returned error: %v", err)
	}

	typed, ok := provider.got.Request.(compatibility.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("provider request type = %T, want compatibility.GenerationCanonicalRequest", provider.got.Request)
	}
	if len(typed.Thread()) != 1 {
		t.Fatalf("thread len = %d, want 1", len(typed.Thread()))
	}
	if len(typed.LastTurn()) != 1 {
		t.Fatalf("last turn len = %d, want 1", len(typed.LastTurn()))
	}
	if got := provider.got.Target.ProtocolKind; got != "responses" {
		t.Fatalf("protocol kind = %q, want %q", got, "responses")
	}
}

func TestRequestPathHandler_DerivesLastTurnForConversationRequestsOntoResponsesTargets(t *testing.T) {
	endpoint := testEndpointWithProtocol(t, protocolsurface.Responses)
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"resp_2",
				"m",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				},
				"completed",
			),
		),
	}
	continuity := &fakeContinuityStore{
		byNamespace: map[compatibility.ContinuationNamespace][]compatibility.ContinuitySnapshot{
			compatibility.NewContinuationNamespace(endpoint.Name().String()): {
				compatibility.NewContinuitySnapshot("resp_prev", "m", []compatibility.CanonicalItem{
					compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
					compatibility.NewTextItem(compatibility.ItemAuthorAssistant, "hello"),
				}),
			},
		},
	}
	orchestrator := requestpath.NewRequestHandler(reader, provider, nil, continuity)

	_, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-5",
		Request: compatibility.NewDialogRequest(
			"m",
			[]compatibility.CanonicalItem{
				compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
				compatibility.NewTextItem(compatibility.ItemAuthorAssistant, "hello"),
				compatibility.NewTextItem(compatibility.ItemAuthorUser, "continue"),
			},
		),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("HandleRequest returned error: %v", err)
	}

	typed, ok := provider.got.Request.(compatibility.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("provider request type = %T, want compatibility.GenerationCanonicalRequest", provider.got.Request)
	}
	if len(typed.Thread()) != 3 {
		t.Fatalf("thread len = %d, want 3", len(typed.Thread()))
	}
	if len(typed.LastTurn()) != 1 {
		t.Fatalf("last turn len = %d, want 1", len(typed.LastTurn()))
	}
	if got := typed.LastTurn()[0].Text; got != "continue" {
		t.Fatalf("last turn text = %q, want %q", got, "continue")
	}
}

func TestRequestPathHandler_IsolatesParallelConversationChainsOnOneEndpoint(t *testing.T) {
	endpoint := testEndpointWithProtocol(t, protocolsurface.Responses)
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"resp_parallel_1",
				"m",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				},
				"completed",
			),
		),
	}
	namespace := compatibility.NewContinuationNamespace(endpoint.Name().String())
	continuity := &fakeContinuityStore{
		byNamespace: map[compatibility.ContinuationNamespace][]compatibility.ContinuitySnapshot{
			namespace: {
				compatibility.NewContinuitySnapshot("resp_a", "m", []compatibility.CanonicalItem{
					compatibility.NewTextItem(compatibility.ItemAuthorUser, "repo a"),
					compatibility.NewTextItem(compatibility.ItemAuthorAssistant, "ready a"),
				}),
				compatibility.NewContinuitySnapshot("resp_b", "m", []compatibility.CanonicalItem{
					compatibility.NewTextItem(compatibility.ItemAuthorUser, "repo b"),
					compatibility.NewTextItem(compatibility.ItemAuthorAssistant, "ready b"),
				}),
			},
		},
	}
	orchestrator := requestpath.NewRequestHandler(reader, provider, nil, continuity)

	_, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-6",
		Request: compatibility.NewDialogRequest(
			"m",
			[]compatibility.CanonicalItem{
				compatibility.NewTextItem(compatibility.ItemAuthorUser, "repo b"),
				compatibility.NewTextItem(compatibility.ItemAuthorAssistant, "ready b"),
				compatibility.NewTextItem(compatibility.ItemAuthorUser, "continue b"),
			},
		),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("HandleRequest returned error: %v", err)
	}

	typed, ok := provider.got.Request.(compatibility.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("provider request type = %T, want compatibility.GenerationCanonicalRequest", provider.got.Request)
	}
	if got := typed.LastTurn()[0].Text; got != "continue b" {
		t.Fatalf("last turn text = %q, want %q", got, "continue b")
	}
	if got := typed.Thread()[0].Text; got != "repo b" {
		t.Fatalf("thread head = %q, want %q", got, "repo b")
	}
}

func TestRequestPathHandler_PreservesNativeResponsesContinuityIDs(t *testing.T) {
	endpoint := testEndpointWithProtocol(t, protocolsurface.Responses)
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"resp_native_1",
				"m",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "ok"),
				},
				"completed",
			),
		),
	}
	continuity := &fakeContinuityStore{}
	orchestrator := requestpath.NewRequestHandler(reader, provider, nil, continuity)

	out, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-4",
		Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:     "m",
			InputText: "hi",
		}),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("HandleRequest returned error: %v", err)
	}

	output, ok := out.Response.Output().(compatibility.CanonicalOutputValue)
	if !ok {
		t.Fatalf("output type = %T, want compatibility.CanonicalOutputValue", out.Response.Output())
	}
	if got := output.ResultID(); got != "resp_native_1" {
		t.Fatalf("output result id = %q, want %q", got, "resp_native_1")
	}
	if len(continuity.stored) != 1 {
		t.Fatalf("stored snapshots = %d, want 1", len(continuity.stored))
	}
	if got := continuity.stored[0].ResponseID; got != "resp_native_1" {
		t.Fatalf("stored response id = %q, want %q", got, "resp_native_1")
	}
}

func TestRequestPathHandler_FailsExplicitlyWhenNativeResponsesParentCannotBeRehydrated(t *testing.T) {
	endpoint := testEndpointWithProtocol(t, protocolsurface.Responses)
	reader := fakeEndpointReader{endpoint: endpoint}
	provider := &fakeProviderExecutor{}
	orchestrator := requestpath.NewRequestHandler(reader, provider, nil, &fakeContinuityStore{})

	_, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-missing-parent",
		Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:              "m",
			PreviousResponseID: "resp_missing",
			InputText:          "continue",
		}),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err == nil {
		t.Fatal("HandleRequest returned nil error, want explicit missing-parent failure")
	}
	var typed compatibility.Error
	if !errors.As(err, &typed) {
		t.Fatalf("error type = %T, want compatibility.Error", err)
	}
	if typed.Code != compatibility.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", typed.Code, compatibility.ErrorCodeBadRequest)
	}
	if typed.Message != "responses previous_response_id could not be rehydrated" {
		t.Fatalf("error message = %q, want explicit responses missing-parent truth", typed.Message)
	}
}

func TestRequestPathHandler_UsesRealContinuityStore(t *testing.T) {
	baseTime := time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	now := baseTime
	store := continuitystore.NewLocalResponseContinuityStore(continuitystore.LocalResponseContinuityStoreConfig{
		Now: func() time.Time { return now },
	})

	endpoint := testEndpointWithProtocol(t, protocolsurface.ChatCompletions)
	reader := fakeEndpointReader{endpoint: endpoint}
	firstProvider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"resp_first",
				"m",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "hello"),
				},
				"completed",
			),
		),
	}
	first := requestpath.NewRequestHandler(reader, firstProvider, nil, store)

	_, err := first.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-first",
		Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:     "m",
			InputText: "hi",
		}),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("first HandleRequest returned error: %v", err)
	}

	now = now.Add(5 * time.Minute)
	secondProvider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"chatcmpl_2",
				"m",
				[]compatibility.OutputItem{
					compatibility.NewTextOutputItem("text_0", "done"),
				},
				"stop",
			),
		),
	}
	second := requestpath.NewRequestHandler(reader, secondProvider, nil, store)

	_, err = second.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-second",
		Request: compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:              "m",
			PreviousResponseID: "resp_first",
			InputText:          "continue",
		}),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("second HandleRequest returned error: %v", err)
	}

	typed, ok := secondProvider.got.Request.(compatibility.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("provider request type = %T, want compatibility.GenerationCanonicalRequest", secondProvider.got.Request)
	}
	if len(typed.Thread()) != 3 {
		t.Fatalf("thread len = %d, want 3", len(typed.Thread()))
	}
	if got := typed.Thread()[1].Text; got != "hello" {
		t.Fatalf("middle thread text = %q, want %q", got, "hello")
	}
	if len(typed.LastTurn()) != 1 || typed.LastTurn()[0].Text != "continue" {
		t.Fatalf("last turn = %#v, want one text item %q", typed.LastTurn(), "continue")
	}
}
